package main

import (
	"fmt"

	vk "github.com/vulkan-go/vulkan"
	"github.com/xlab/android-go/android"
	"github.com/xlab/tablewriter"
)

type VulkanDeviceInfo struct {
	gpuDevices []vk.PhysicalDevice

	instance vk.Instance
	surface  vk.Surface
	device   vk.Device
}

func NewVulkanDevice(appInfo *vk.ApplicationInfo,
	window *android.NativeWindow) (*VulkanDeviceInfo, error) {

	v := &VulkanDeviceInfo{}

	// step 1: create a Vulkan instance.
	instanceExtensions := []string{
		"VK_KHR_surface\x00",
		"VK_KHR_android_surface\x00",
	}
	instanceCreateInfo := &vk.InstanceCreateInfo{
		SType:                   vk.StructureTypeInstanceCreateInfo,
		PApplicationInfo:        appInfo,
		EnabledExtensionCount:   uint32(len(instanceExtensions)),
		PpEnabledExtensionNames: instanceExtensions,
	}
	err := vk.Error(vk.CreateInstance(instanceCreateInfo, nil, &v.instance))
	if err != nil {
		err = fmt.Errorf("vkCreateInstance failed with %s", err)
		return nil, err
	}

	// step 2: init the surface using an Android native window.
	createInfo := &vk.AndroidSurfaceCreateInfo{
		SType:  vk.StructureTypeAndroidSurfaceCreateInfo,
		Window: (*vk.ANativeWindow)(window),
	}
	err = vk.Error(vk.CreateAndroidSurface(v.instance, createInfo, nil, &v.surface))
	if err != nil {
		vk.DestroyInstance(v.instance, nil)
		err = fmt.Errorf("vkCreateAndroidSurface failed with %s", err)
		return nil, err
	}
	if v.gpuDevices, err = getPhysicalDevices(v.instance); err != nil {
		v.gpuDevices = nil
		vk.DestroySurface(v.instance, v.surface, nil)
		vk.DestroyInstance(v.instance, nil)
		return nil, err
	}

	// step 3: create a logical device from the first GPU available.
	queueCreateInfos := []vk.DeviceQueueCreateInfo{{
		SType:            vk.StructureTypeDeviceQueueCreateInfo,
		QueueCount:       1,
		PQueuePriorities: []float32{1.0},
	}}
	deviceExtensions := []string{
		"VK_KHR_swapchain\x00",
	}
	deviceCreateInfo := &vk.DeviceCreateInfo{
		SType:                   vk.StructureTypeDeviceCreateInfo,
		QueueCreateInfoCount:    uint32(len(queueCreateInfos)),
		PQueueCreateInfos:       queueCreateInfos,
		EnabledExtensionCount:   uint32(len(deviceExtensions)),
		PpEnabledExtensionNames: deviceExtensions,
	}
	var device vk.Device
	err = vk.Error(vk.CreateDevice(v.gpuDevices[0], deviceCreateInfo, nil, &device))
	if err != nil {
		v.gpuDevices = nil
		vk.DestroySurface(v.instance, v.surface, nil)
		vk.DestroyInstance(v.instance, nil)
		err = fmt.Errorf("vkCreateDevice failed with %s", err)
		return nil, err
	} else {
		v.device = device
	}

	return v, nil
}

func (v *VulkanDeviceInfo) Destroy() {
	if v == nil {
		return
	}
	v.gpuDevices = nil
	vk.DestroySurface(v.instance, v.surface, nil)
	vk.DestroyDevice(v.device, nil)
	vk.DestroyInstance(v.instance, nil)
}

func getPhysicalDevices(instance vk.Instance) ([]vk.PhysicalDevice, error) {
	var gpuCount uint32
	err := vk.Error(vk.EnumeratePhysicalDevices(instance, &gpuCount, nil))
	if err != nil {
		err = fmt.Errorf("vkEnumeratePhysicalDevices failed with %s", err)
		return nil, err
	}
	if gpuCount == 0 {
		err = fmt.Errorf("getPhysicalDevice: no GPUs found on the system")
		return nil, err
	}
	gpuList := make([]vk.PhysicalDevice, gpuCount)
	err = vk.Error(vk.EnumeratePhysicalDevices(instance, &gpuCount, gpuList))
	if err != nil {
		err = fmt.Errorf("vkEnumeratePhysicalDevices failed with %s", err)
		return nil, err
	}
	return gpuList, nil
}

func printInfo(v *VulkanDeviceInfo) {
	var gpuProperties vk.PhysicalDeviceProperties
	vk.GetPhysicalDeviceProperties(v.gpuDevices[0], &gpuProperties)
	gpuProperties.Deref()

	table := tablewriter.CreateTable()
	table.UTF8Box()
	table.AddTitle("VULKAN PROPERTIES AND SURFACE CAPABILITES")
	table.AddRow("Physical Device Name", vk.ToString(gpuProperties.DeviceName[:]))
	table.AddRow("Physical Device Vendor", fmt.Sprintf("%x", gpuProperties.VendorID))
	if gpuProperties.DeviceType != vk.PhysicalDeviceTypeOther {
		table.AddRow("Physical Device Type", physicalDeviceType(gpuProperties.DeviceType))
	}
	table.AddRow("Physical GPUs", len(v.gpuDevices))
	table.AddRow("API Version", vk.Version(gpuProperties.ApiVersion))
	table.AddRow("API Version Supported", vk.Version(gpuProperties.ApiVersion))
	table.AddRow("Driver Version", vk.Version(gpuProperties.DriverVersion))

	var surfaceCapabilities vk.SurfaceCapabilities
	vk.GetPhysicalDeviceSurfaceCapabilities(v.gpuDevices[0], v.surface, &surfaceCapabilities)
	surfaceCapabilities.Deref()
	surfaceCapabilities.CurrentExtent.Deref()
	surfaceCapabilities.MinImageExtent.Deref()
	surfaceCapabilities.MaxImageExtent.Deref()

	table.AddSeparator()
	table.AddRow("Image count", fmt.Sprintf("%d - %d",
		surfaceCapabilities.MinImageCount, surfaceCapabilities.MaxImageCount))
	table.AddRow("Array layers", fmt.Sprintf("%d",
		surfaceCapabilities.MaxImageArrayLayers))
	table.AddRow("Image size (current)", fmt.Sprintf("%dx%d",
		surfaceCapabilities.CurrentExtent.Width, surfaceCapabilities.CurrentExtent.Height))
	table.AddRow("Image size (extent)", fmt.Sprintf("%dx%d - %dx%d",
		surfaceCapabilities.MinImageExtent.Width, surfaceCapabilities.MinImageExtent.Height,
		surfaceCapabilities.MaxImageExtent.Width, surfaceCapabilities.MaxImageExtent.Height))
	table.AddRow("Usage flags", fmt.Sprintf("%02x",
		surfaceCapabilities.SupportedUsageFlags))
	table.AddRow("Current transform", fmt.Sprintf("%02x",
		surfaceCapabilities.CurrentTransform))
	table.AddRow("Allowed transforms", fmt.Sprintf("%02x",
		surfaceCapabilities.SupportedTransforms))

	table.AddSeparator()
	var formatCount uint32
	vk.GetPhysicalDeviceSurfaceFormats(v.gpuDevices[0], v.surface, &formatCount, nil)
	table.AddRow("Surface formats", fmt.Sprintf("%d of %d", formatCount, vk.FormatRangeSize))

	fmt.Println("\n\n" + table.Render())
}

func physicalDeviceType(dev vk.PhysicalDeviceType) string {
	switch dev {
	case vk.PhysicalDeviceTypeIntegratedGpu:
		return "Integrated GPU"
	case vk.PhysicalDeviceTypeDiscreteGpu:
		return "Discrete GPU"
	case vk.PhysicalDeviceTypeVirtualGpu:
		return "Virtual GPU"
	case vk.PhysicalDeviceTypeCpu:
		return "CPU"
	case vk.PhysicalDeviceTypeOther:
		return "Other"
	default:
		return "Unknown"
	}
}