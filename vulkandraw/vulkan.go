package main

import (
	"fmt"
	"log"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
	"github.com/xlab/android-go/android"
)

// enableDebug is disabled by default since VK_EXT_debug_report
// is not guaranteed to be present on a device.
//
// Nvidia Shield K1 fw 1.3.0 lacks this extension,
// on fw 1.2.0 it works fine.
const enableDebug = false

type VulkanDeviceInfo struct {
	gpuDevices []vk.PhysicalDevice

	dbg      vk.DebugReportCallback
	instance vk.Instance
	surface  vk.Surface
	queue    vk.Queue
	device   vk.Device
}

type VulkanSwapchainInfo struct {
	device vk.Device

	swapchains   []vk.Swapchain
	swapchainLen []uint32

	displaySize   vk.Extent2D
	displayFormat vk.Format

	framebuffers []vk.Framebuffer
	displayViews []vk.ImageView
}

func (v *VulkanSwapchainInfo) DefaultSwapchain() vk.Swapchain {
	return v.swapchains[0]
}

func (v *VulkanSwapchainInfo) DefaultSwapchainLen() uint32 {
	return v.swapchainLen[0]
}

type VulkanBufferInfo struct {
	device        vk.Device
	vertexBuffers []vk.Buffer
}

func (v *VulkanBufferInfo) DefaultVertexBuffer() vk.Buffer {
	return v.vertexBuffers[0]
}

type VulkanGfxPipelineInfo struct {
	device vk.Device

	layout   vk.PipelineLayout
	cache    vk.PipelineCache
	pipeline vk.Pipeline
}

type VulkanRenderInfo struct {
	device vk.Device

	renderPass vk.RenderPass
	cmdPool    vk.CommandPool
	cmdBuffers []vk.CommandBuffer
	semaphores []vk.Semaphore
	fences     []vk.Fence
}

func (v *VulkanRenderInfo) DefaultFence() vk.Fence {
	return v.fences[0]
}

func (v *VulkanRenderInfo) DefaultSemaphore() vk.Semaphore {
	return v.semaphores[0]
}

func VulkanInit(v *VulkanDeviceInfo, s *VulkanSwapchainInfo,
	r *VulkanRenderInfo, b *VulkanBufferInfo, gfx *VulkanGfxPipelineInfo) {

	clearValues := []vk.ClearValue{
		vk.NewClearValue([]float32{0.098, 0.71, 0.996, 1}),
	}
	for i := range r.cmdBuffers {
		cmdBufferBeginInfo := vk.CommandBufferBeginInfo{
			SType: vk.StructureTypeCommandBufferBeginInfo,
		}
		renderPassBeginInfo := vk.RenderPassBeginInfo{
			SType:       vk.StructureTypeRenderPassBeginInfo,
			RenderPass:  r.renderPass,
			Framebuffer: s.framebuffers[i],
			RenderArea: vk.Rect2D{
				Offset: vk.Offset2D{
					X: 0, Y: 0,
				},
				Extent: s.displaySize,
			},
			ClearValueCount: 1,
			PClearValues:    clearValues,
		}
		ret := vk.BeginCommandBuffer(r.cmdBuffers[i], &cmdBufferBeginInfo)
		check(ret, "vk.BeginCommandBuffer")

		vk.CmdBeginRenderPass(r.cmdBuffers[i], &renderPassBeginInfo, vk.SubpassContentsInline)
		vk.CmdBindPipeline(r.cmdBuffers[i], vk.PipelineBindPointGraphics, gfx.pipeline)
		offsets := make([]vk.DeviceSize, len(b.vertexBuffers))
		vk.CmdBindVertexBuffers(r.cmdBuffers[i], 0, 1, b.vertexBuffers, offsets)
		vk.CmdDraw(r.cmdBuffers[i], 3, 1, 0, 0)
		vk.CmdEndRenderPass(r.cmdBuffers[i])

		ret = vk.EndCommandBuffer(r.cmdBuffers[i])
		check(ret, "vk.EndCommandBuffer")
	}
	fenceCreateInfo := vk.FenceCreateInfo{
		SType: vk.StructureTypeFenceCreateInfo,
	}
	semaphoreCreateInfo := vk.SemaphoreCreateInfo{
		SType: vk.StructureTypeSemaphoreCreateInfo,
	}
	r.fences = make([]vk.Fence, 1)
	ret := vk.CreateFence(v.device, &fenceCreateInfo, nil, &r.fences[0])
	check(ret, "vk.CreateFence")
	r.semaphores = make([]vk.Semaphore, 1)
	ret = vk.CreateSemaphore(v.device, &semaphoreCreateInfo, nil, &r.semaphores[0])
	check(ret, "vk.CreateSemaphore")
}

func VulkanDrawFrame(v VulkanDeviceInfo,
	s VulkanSwapchainInfo, r VulkanRenderInfo) bool {
	var nextIdx uint32

	// Phase 1: vk.AcquireNextImage
	// 			get the framebuffer index we should draw in
	//
	//			N.B. non-infinite timeouts may be not yet implemented
	//			by your Vulkan driver

	err := vk.Error(vk.AcquireNextImage(v.device, s.DefaultSwapchain(),
		vk.MaxUint64, r.DefaultSemaphore(), vk.NullHandle, &nextIdx))
	if err != nil {
		err = fmt.Errorf("vk.AcquireNextImage failed with %s", err)
		log.Println("[WARN]", err)
		return false
	}

	// Phase 2: vk.QueueSubmit
	//			vk.WaitForFences

	vk.ResetFences(v.device, 1, r.fences)
	submitInfo := []vk.SubmitInfo{{
		SType:              vk.StructureTypeSubmitInfo,
		WaitSemaphoreCount: 1,
		PWaitSemaphores:    r.semaphores,
		CommandBufferCount: 1,
		PCommandBuffers:    r.cmdBuffers[nextIdx:],
	}}
	err = vk.Error(vk.QueueSubmit(v.queue, 1, submitInfo, r.DefaultFence()))
	if err != nil {
		err = fmt.Errorf("vk.QueueSubmit failed with %s", err)
		log.Println("[WARN]", err)
		return false
	}

	const timeoutNano = 10 * 1000 * 1000 * 1000 // 10 sec
	err = vk.Error(vk.WaitForFences(v.device, 1, r.fences, vk.True, timeoutNano))
	if err != nil {
		err = fmt.Errorf("vk.WaitForFences failed with %s", err)
		log.Println("[WARN]", err)
		return false
	}

	// Phase 3: vk.QueuePresent

	imageIndices := []uint32{nextIdx}
	presentInfo := vk.PresentInfo{
		SType:          vk.StructureTypePresentInfo,
		SwapchainCount: 1,
		PSwapchains:    s.swapchains,
		PImageIndices:  imageIndices,
	}
	err = vk.Error(vk.QueuePresent(v.queue, &presentInfo))
	if err != nil {
		err = fmt.Errorf("vk.QueuePresent failed with %s", err)
		log.Println("[WARN]", err)
		return false
	}
	return true
}

func (r *VulkanRenderInfo) CreateCommandBuffers(n uint32) error {
	r.cmdBuffers = make([]vk.CommandBuffer, n)
	cmdBufferAllocateInfo := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        r.cmdPool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: n,
	}
	err := vk.Error(vk.AllocateCommandBuffers(r.device, &cmdBufferAllocateInfo, r.cmdBuffers))
	if err != nil {
		err = fmt.Errorf("vk.AllocateCommandBuffers failed with %s", err)
		return err
	}
	return nil
}

func CreateRenderer(device vk.Device, displayFormat vk.Format) (VulkanRenderInfo, error) {
	attachmentDescriptions := []vk.AttachmentDescription{{
		Format:         displayFormat,
		Samples:        vk.SampleCount1Bit,
		LoadOp:         vk.AttachmentLoadOpClear,
		StoreOp:        vk.AttachmentStoreOpStore,
		StencilLoadOp:  vk.AttachmentLoadOpDontCare,
		StencilStoreOp: vk.AttachmentStoreOpDontCare,
		InitialLayout:  vk.ImageLayoutColorAttachmentOptimal,
		FinalLayout:    vk.ImageLayoutColorAttachmentOptimal,
	}}
	colorAttachments := []vk.AttachmentReference{{
		Attachment: 0,
		Layout:     vk.ImageLayoutColorAttachmentOptimal,
	}}
	subpassDescriptions := []vk.SubpassDescription{{
		PipelineBindPoint:    vk.PipelineBindPointGraphics,
		ColorAttachmentCount: 1,
		PColorAttachments:    colorAttachments,
	}}
	renderPassCreateInfo := vk.RenderPassCreateInfo{
		SType:           vk.StructureTypeRenderPassCreateInfo,
		AttachmentCount: 1,
		PAttachments:    attachmentDescriptions,
		SubpassCount:    1,
		PSubpasses:      subpassDescriptions,
	}
	cmdPoolCreateInfo := vk.CommandPoolCreateInfo{
		SType:            vk.StructureTypeCommandPoolCreateInfo,
		Flags:            vk.CommandPoolCreateFlags(vk.CommandPoolCreateResetCommandBufferBit),
		QueueFamilyIndex: 0,
	}
	var r VulkanRenderInfo
	err := vk.Error(vk.CreateRenderPass(device, &renderPassCreateInfo, nil, &r.renderPass))
	if err != nil {
		err = fmt.Errorf("vk.CreateRenderPass failed with %s", err)
		return r, err
	}
	err = vk.Error(vk.CreateCommandPool(device, &cmdPoolCreateInfo, nil, &r.cmdPool))
	if err != nil {
		err = fmt.Errorf("vk.CreateCommandPool failed with %s", err)
		return r, err
	}
	r.device = device
	return r, nil
}

func NewVulkanDeviceAndroid(appInfo vk.ApplicationInfo,
	window *android.NativeWindow) (VulkanDeviceInfo, error) {

	// Phase 1: vk.CreateInstance with vk.InstanceCreateInfo

	existingExtensions := getInstanceExtensions()
	log.Println("[INFO] Instance extensions:", existingExtensions)

	instanceExtensions := []string{
		"VK_KHR_surface\x00",
		"VK_KHR_android_surface\x00",
	}
	if enableDebug {
		instanceExtensions = append(instanceExtensions,
			"VK_EXT_debug_report\x00")
	}

	// these layers must be included in APK,
	// see Android.mk and ValidationLayers.mk
	instanceLayers := []string{
	// "VK_LAYER_GOOGLE_threading\x00",
	// "VK_LAYER_LUNARG_parameter_validation\x00",
	// "VK_LAYER_LUNARG_object_tracker\x00",
	// "VK_LAYER_LUNARG_core_validation\x00",
	// "VK_LAYER_LUNARG_api_dump\x00",
	// "VK_LAYER_LUNARG_image\x00",
	// "VK_LAYER_LUNARG_swapchain\x00",
	// "VK_LAYER_GOOGLE_unique_objects\x00",
	}

	instanceCreateInfo := vk.InstanceCreateInfo{
		SType:                   vk.StructureTypeInstanceCreateInfo,
		PApplicationInfo:        &appInfo,
		EnabledExtensionCount:   uint32(len(instanceExtensions)),
		PpEnabledExtensionNames: instanceExtensions,
		EnabledLayerCount:       uint32(len(instanceLayers)),
		PpEnabledLayerNames:     instanceLayers,
	}
	var v VulkanDeviceInfo
	err := vk.Error(vk.CreateInstance(&instanceCreateInfo, nil, &v.instance))
	if err != nil {
		err = fmt.Errorf("vk.CreateInstance failed with %s", err)
		return v, err
	}

	// Phase 2: vk.CreateAndroidSurface with vk.AndroidSurfaceCreateInfo

	surfaceCreateInfo := vk.AndroidSurfaceCreateInfo{
		SType:  vk.StructureTypeAndroidSurfaceCreateInfo,
		Window: (*vk.ANativeWindow)(window),
	}
	err = vk.Error(vk.CreateAndroidSurface(v.instance, &surfaceCreateInfo, nil, &v.surface))
	if err != nil {
		vk.DestroyInstance(v.instance, nil)
		err = fmt.Errorf("vk.CreateAndroidSurface failed with %s", err)
		return v, err
	}
	if v.gpuDevices, err = getPhysicalDevices(v.instance); err != nil {
		v.gpuDevices = nil
		vk.DestroySurface(v.instance, v.surface, nil)
		vk.DestroyInstance(v.instance, nil)
		return v, err
	}

	existingExtensions = getDeviceExtensions(v.gpuDevices[0])
	log.Println("[INFO] Device extensions:", existingExtensions)

	// Phase 3: vk.CreateDevice with vk.DeviceCreateInfo (a logical device)

	// these layers must be included in APK,
	// see Android.mk and ValidationLayers.mk
	deviceLayers := []string{
	// "VK_LAYER_GOOGLE_threading\x00",
	// "VK_LAYER_LUNARG_parameter_validation\x00",
	// "VK_LAYER_LUNARG_object_tracker\x00",
	// "VK_LAYER_LUNARG_core_validation\x00",
	// "VK_LAYER_LUNARG_api_dump\x00",
	// "VK_LAYER_LUNARG_image\x00",
	// "VK_LAYER_LUNARG_swapchain\x00",
	// "VK_LAYER_GOOGLE_unique_objects\x00",
	}

	queueCreateInfos := []vk.DeviceQueueCreateInfo{{
		SType:            vk.StructureTypeDeviceQueueCreateInfo,
		QueueCount:       1,
		PQueuePriorities: []float32{1.0},
	}}
	deviceExtensions := []string{
		"VK_KHR_swapchain\x00",
	}
	deviceCreateInfo := vk.DeviceCreateInfo{
		SType:                   vk.StructureTypeDeviceCreateInfo,
		QueueCreateInfoCount:    uint32(len(queueCreateInfos)),
		PQueueCreateInfos:       queueCreateInfos,
		EnabledExtensionCount:   uint32(len(deviceExtensions)),
		PpEnabledExtensionNames: deviceExtensions,
		EnabledLayerCount:       uint32(len(deviceLayers)),
		PpEnabledLayerNames:     deviceLayers,
	}
	var device vk.Device // we choose the first GPU available for this device
	err = vk.Error(vk.CreateDevice(v.gpuDevices[0], &deviceCreateInfo, nil, &device))
	if err != nil {
		v.gpuDevices = nil
		vk.DestroySurface(v.instance, v.surface, nil)
		vk.DestroyInstance(v.instance, nil)
		err = fmt.Errorf("vk.CreateDevice failed with %s", err)
		return v, err
	} else {
		v.device = device
		var queue vk.Queue
		vk.GetDeviceQueue(device, 0, 0, &queue)
		v.queue = queue
	}

	if enableDebug {
		// Phase 4: vk.CreateDebugReportCallback

		dbgCreateInfo := vk.DebugReportCallbackCreateInfo{
			SType:       vk.StructureTypeDebugReportCallbackCreateInfo,
			Flags:       vk.DebugReportFlags(vk.DebugReportErrorBit | vk.DebugReportWarningBit),
			PfnCallback: dbgCallbackFunc,
		}
		var dbg vk.DebugReportCallback
		err = vk.Error(vk.CreateDebugReportCallback(v.instance, &dbgCreateInfo, nil, &dbg))
		if err != nil {
			err = fmt.Errorf("vk.CreateDebugReportCallback failed with %s", err)
			log.Println("[WARN]", err)
			return v, nil
		}
		v.dbg = dbg
	}
	return v, nil
}

func getInstanceExtensions() (extNames []string) {
	var instanceExtLen uint32
	ret := vk.EnumerateInstanceExtensionProperties("", &instanceExtLen, nil)
	check(ret, "vk.EnumerateInstanceExtensionProperties")
	instanceExt := make([]vk.ExtensionProperties, instanceExtLen)
	ret = vk.EnumerateInstanceExtensionProperties("", &instanceExtLen, instanceExt)
	check(ret, "vk.EnumerateInstanceExtensionProperties")
	for _, ext := range instanceExt {
		ext.Deref()
		extNames = append(extNames,
			vk.ToString(ext.ExtensionName[:]))
	}
	return extNames
}

func getDeviceExtensions(gpu vk.PhysicalDevice) (extNames []string) {
	var deviceExtLen uint32
	ret := vk.EnumerateDeviceExtensionProperties(gpu, "", &deviceExtLen, nil)
	check(ret, "vk.EnumerateDeviceExtensionProperties")
	deviceExt := make([]vk.ExtensionProperties, deviceExtLen)
	ret = vk.EnumerateDeviceExtensionProperties(gpu, "", &deviceExtLen, deviceExt)
	check(ret, "vk.EnumerateDeviceExtensionProperties")
	for _, ext := range deviceExt {
		ext.Deref()
		extNames = append(extNames,
			vk.ToString(ext.ExtensionName[:]))
	}
	return extNames
}

func dbgCallbackFunc(flags vk.DebugReportFlags, objectType vk.DebugReportObjectType,
	object uint64, location uint, messageCode int32, pLayerPrefix string,
	pMessage string, pUserData unsafe.Pointer) vk.Bool32 {

	switch {
	case flags&vk.DebugReportFlags(vk.DebugReportErrorBit) != 0:
		log.Printf("[ERROR %d] %s on layer %s", messageCode, pMessage, pLayerPrefix)
	case flags&vk.DebugReportFlags(vk.DebugReportWarningBit) != 0:
		log.Printf("[WARN %d] %s on layer %s", messageCode, pMessage, pLayerPrefix)
	default:
		log.Printf("[WARN] unknown debug message %d (layer %s)", messageCode, pLayerPrefix)
	}
	return vk.Bool32(vk.False)
}

func getPhysicalDevices(instance vk.Instance) ([]vk.PhysicalDevice, error) {
	var gpuCount uint32
	err := vk.Error(vk.EnumeratePhysicalDevices(instance, &gpuCount, nil))
	if err != nil {
		err = fmt.Errorf("vk.EnumeratePhysicalDevices failed with %s", err)
		return nil, err
	}
	if gpuCount == 0 {
		err = fmt.Errorf("getPhysicalDevice: no GPUs found on the system")
		return nil, err
	}
	gpuList := make([]vk.PhysicalDevice, gpuCount)
	err = vk.Error(vk.EnumeratePhysicalDevices(instance, &gpuCount, gpuList))
	if err != nil {
		err = fmt.Errorf("vk.EnumeratePhysicalDevices failed with %s", err)
		return nil, err
	}
	return gpuList, nil
}

func (v *VulkanDeviceInfo) CreateSwapchain() (VulkanSwapchainInfo, error) {
	gpu := v.gpuDevices[0]

	// Phase 1: vk.GetPhysicalDeviceSurfaceCapabilities
	//			vk.GetPhysicalDeviceSurfaceFormats

	var s VulkanSwapchainInfo
	var surfaceCapabilities vk.SurfaceCapabilities
	err := vk.Error(vk.GetPhysicalDeviceSurfaceCapabilities(gpu, v.surface, &surfaceCapabilities))
	if err != nil {
		err = fmt.Errorf("vk.GetPhysicalDeviceSurfaceCapabilities failed with %s", err)
		return s, err
	}
	var formatCount uint32
	vk.GetPhysicalDeviceSurfaceFormats(gpu, v.surface, &formatCount, nil)
	formats := make([]vk.SurfaceFormat, formatCount)
	vk.GetPhysicalDeviceSurfaceFormats(gpu, v.surface, &formatCount, formats)

	log.Println("[INFO] got", formatCount, "physical device surface formats")

	chosenFormat := -1
	for i := 0; i < int(formatCount); i++ {
		formats[i].Deref()
		if formats[i].Format == vk.FormatR8g8b8a8Unorm {
			chosenFormat = i
			break
		}
	}
	if chosenFormat < 0 {
		err := fmt.Errorf("vk.GetPhysicalDeviceSurfaceFormats not found vk.FormatR8g8b8a8Unorm format")
		return s, err
	}

	// Phase 2: vk.CreateSwapchain
	//			create a swapchain with supported capabilities and format

	surfaceCapabilities.Deref()
	s.displaySize = surfaceCapabilities.CurrentExtent
	s.displaySize.Deref()
	s.displayFormat = formats[chosenFormat].Format
	queueFamily := []uint32{0}
	swapchainCreateInfo := vk.SwapchainCreateInfo{
		SType:           vk.StructureTypeSwapchainCreateInfo,
		Surface:         v.surface,
		MinImageCount:   surfaceCapabilities.MinImageCount,
		ImageFormat:     formats[chosenFormat].Format,
		ImageColorSpace: formats[chosenFormat].ColorSpace,
		ImageExtent:     surfaceCapabilities.CurrentExtent,
		ImageUsage:      vk.ImageUsageFlags(vk.ImageUsageColorAttachmentBit),
		PreTransform:    vk.SurfaceTransformIdentityBit,

		ImageArrayLayers:      1,
		ImageSharingMode:      vk.SharingModeExclusive,
		QueueFamilyIndexCount: 1,
		PQueueFamilyIndices:   queueFamily,
		PresentMode:           vk.PresentModeFifo,
		OldSwapchain:          vk.NullHandle,
		Clipped:               vk.False,
	}
	s.swapchains = make([]vk.Swapchain, 1)
	err = vk.Error(vk.CreateSwapchain(v.device, &swapchainCreateInfo, nil, &s.swapchains[0]))
	if err != nil {
		err = fmt.Errorf("vk.CreateSwapchain failed with %s", err)
		return s, err
	}
	s.swapchainLen = make([]uint32, 1)
	err = vk.Error(vk.GetSwapchainImages(v.device, s.DefaultSwapchain(), &s.swapchainLen[0], nil))
	if err != nil {
		err = fmt.Errorf("vk.GetSwapchainImages failed with %s", err)
		return s, err
	}
	for i := range formats {
		formats[i].Free()
	}
	s.device = v.device
	return s, nil
}

func (s *VulkanSwapchainInfo) CreateFramebuffers(renderPass vk.RenderPass, depthView vk.ImageView) error {
	// Phase 1: vk.GetSwapchainImages

	var swapchainImagesCount uint32
	err := vk.Error(vk.GetSwapchainImages(s.device, s.DefaultSwapchain(), &swapchainImagesCount, nil))
	if err != nil {
		err = fmt.Errorf("vk.GetSwapchainImages failed with %s", err)
		return err
	}
	swapchainImages := make([]vk.Image, swapchainImagesCount)
	vk.GetSwapchainImages(s.device, s.DefaultSwapchain(), &swapchainImagesCount, swapchainImages)

	// Phase 2: vk.CreateImageView
	//			create image view for each swapchain image

	s.displayViews = make([]vk.ImageView, len(swapchainImages))
	for i := range s.displayViews {
		viewCreateInfo := vk.ImageViewCreateInfo{
			SType:    vk.StructureTypeImageViewCreateInfo,
			Image:    swapchainImages[i],
			ViewType: vk.ImageViewType2d,
			Format:   s.displayFormat,
			Components: vk.ComponentMapping{
				R: vk.ComponentSwizzleR,
				G: vk.ComponentSwizzleG,
				B: vk.ComponentSwizzleB,
				A: vk.ComponentSwizzleA,
			},
			SubresourceRange: vk.ImageSubresourceRange{
				AspectMask: vk.ImageAspectFlags(vk.ImageAspectColorBit),
				LevelCount: 1,
				LayerCount: 1,
			},
		}
		err := vk.Error(vk.CreateImageView(s.device, &viewCreateInfo, nil, &s.displayViews[i]))
		if err != nil {
			err = fmt.Errorf("vk.CreateImageView failed with %s", err)
			return err // bail out
		}
	}
	swapchainImages = nil

	// Phase 3: vk.CreateFramebuffer
	//			create a framebuffer from each swapchain image

	s.framebuffers = make([]vk.Framebuffer, s.DefaultSwapchainLen())
	for i := range s.framebuffers {
		attachments := []vk.ImageView{
			s.displayViews[i], depthView,
		}
		fbCreateInfo := vk.FramebufferCreateInfo{
			SType:           vk.StructureTypeFramebufferCreateInfo,
			RenderPass:      renderPass,
			Layers:          1,
			AttachmentCount: 1, // 2 if has depthView
			PAttachments:    attachments,
			Width:           s.displaySize.Width,
			Height:          s.displaySize.Height,
		}
		if depthView != vk.NullHandle {
			fbCreateInfo.AttachmentCount = 2
		}
		err := vk.Error(vk.CreateFramebuffer(s.device, &fbCreateInfo, nil, &s.framebuffers[i]))
		if err != nil {
			err = fmt.Errorf("vk.CreateFramebuffer failed with %s", err)
			return err // bail out
		}
	}
	return nil
}

func (v VulkanDeviceInfo) CreateBuffers() (VulkanBufferInfo, error) {
	gpu := v.gpuDevices[0]

	// Phase 1: vk.CreateBuffer
	//			create the triangle vertex buffer

	vertexData := []float32{
		-1, -1, 0,
		1, -1, 0,
		0, 1, 0,
	}
	vertexDataSize := 4 * len(vertexData)
	queueFamilyIdx := []uint32{0}
	bufferCreateInfo := vk.BufferCreateInfo{
		SType:                 vk.StructureTypeBufferCreateInfo,
		Size:                  vk.DeviceSize(vertexDataSize),
		Usage:                 vk.BufferUsageFlags(vk.BufferUsageVertexBufferBit),
		SharingMode:           vk.SharingModeExclusive,
		QueueFamilyIndexCount: 1,
		PQueueFamilyIndices:   queueFamilyIdx,
	}
	buffer := VulkanBufferInfo{
		vertexBuffers: make([]vk.Buffer, 1),
	}
	err := vk.Error(vk.CreateBuffer(v.device, &bufferCreateInfo, nil, &buffer.vertexBuffers[0]))
	if err != nil {
		err = fmt.Errorf("vk.CreateBuffer failed with %s", err)
		return buffer, err
	}

	// Phase 2: vk.GetBufferMemoryRequirements
	//			vk.FindMemoryTypeIndex
	// 			assign a proper memory type for that buffer

	var memReq vk.MemoryRequirements
	vk.GetBufferMemoryRequirements(v.device, buffer.DefaultVertexBuffer(), &memReq)
	memReq.Deref()
	allocInfo := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  memReq.Size,
		MemoryTypeIndex: 0, // see below
	}
	allocInfo.MemoryTypeIndex, _ = vk.FindMemoryTypeIndex(gpu, memReq.MemoryTypeBits,
		vk.MemoryPropertyHostVisibleBit)

	// Phase 3: vk.AllocateMemory
	//			vk.MapMemory
	//			vk.MemCopyFloat32
	//			vk.UnmapMemory
	// 			allocate and map memory for that buffer

	var deviceMemory vk.DeviceMemory
	err = vk.Error(vk.AllocateMemory(v.device, &allocInfo, nil, &deviceMemory))
	if err != nil {
		err = fmt.Errorf("vk.AllocateMemory failed with %s", err)
		return buffer, err
	}
	var data unsafe.Pointer
	vk.MapMemory(v.device, deviceMemory, 0, vk.DeviceSize(vertexDataSize), 0, &data)
	n := vk.MemCopyFloat32(data, vertexData)
	if n != len(vertexData) {
		log.Println("[WARN] failed to copy vertex buffer data")
	}
	vk.UnmapMemory(v.device, deviceMemory)

	// Phase 4: vk.BindBufferMemory
	//			copy vertex data and bind buffer

	err = vk.Error(vk.BindBufferMemory(v.device, buffer.DefaultVertexBuffer(), deviceMemory, 0))
	if err != nil {
		err = fmt.Errorf("vk.BindBufferMemory failed with %s", err)
		return buffer, err
	}
	buffer.device = v.device
	return buffer, err
}

func (buf *VulkanBufferInfo) Destroy() {
	for i := range buf.vertexBuffers {
		vk.DestroyBuffer(buf.device, buf.vertexBuffers[i], nil)
	}
}

func LoadShader(device vk.Device, name string) (vk.ShaderModule, error) {
	var module vk.ShaderModule
	data, err := Asset(name)
	if err != nil {
		err := fmt.Errorf("asset %s not found: %s", name, err)
		return module, err
	}

	// Phase 1: vk.CreateShaderModule

	shaderModuleCreateInfo := vk.ShaderModuleCreateInfo{
		SType:    vk.StructureTypeShaderModuleCreateInfo,
		CodeSize: uint(len(data)),
		PCode:    repackUint32(data),
	}
	err = vk.Error(vk.CreateShaderModule(device, &shaderModuleCreateInfo, nil, &module))
	if err != nil {
		err = fmt.Errorf("vk.CreateShaderModule failed with %s", err)
		return module, err
	}
	return module, nil
}

func CreateGraphicsPipeline(device vk.Device,
	displaySize vk.Extent2D, renderPass vk.RenderPass) (VulkanGfxPipelineInfo, error) {

	var gfxPipeline VulkanGfxPipelineInfo

	// Phase 1: vk.CreatePipelineLayout
	//			create pipeline layout (empty)

	pipelineLayoutCreateInfo := vk.PipelineLayoutCreateInfo{
		SType: vk.StructureTypePipelineLayoutCreateInfo,
	}
	err := vk.Error(vk.CreatePipelineLayout(device, &pipelineLayoutCreateInfo, nil, &gfxPipeline.layout))
	if err != nil {
		err = fmt.Errorf("vk.CreatePipelineLayout failed with %s", err)
		return gfxPipeline, err
	}
	dynamicState := vk.PipelineDynamicStateCreateInfo{
		SType: vk.StructureTypePipelineDynamicStateCreateInfo,
		// no dynamic state for this demo
	}

	// Phase 2: load shaders and specify shader stages

	vertexShader, err := LoadShader(device, "shaders/tri-vert.spv")
	if err != nil { // err has enough info
		return gfxPipeline, err
	}
	defer vk.DestroyShaderModule(device, vertexShader, nil)

	fragmentShader, err := LoadShader(device, "shaders/tri-frag.spv")
	if err != nil { // err has enough info
		return gfxPipeline, err
	}
	defer vk.DestroyShaderModule(device, fragmentShader, nil)

	shaderStages := []vk.PipelineShaderStageCreateInfo{
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageVertexBit,
			Module: vertexShader,
			PName:  "main\x00",
		},
		{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageFragmentBit,
			Module: fragmentShader,
			PName:  "main\x00",
		},
	}

	// Phase 3: specify viewport state

	viewports := []vk.Viewport{{
		MinDepth: 0.0,
		MaxDepth: 1.0,
		X:        0,
		Y:        0,
		Width:    float32(displaySize.Width),
		Height:   float32(displaySize.Height),
	}}
	scissors := []vk.Rect2D{{
		Extent: displaySize,
		Offset: vk.Offset2D{
			X: 0, Y: 0,
		},
	}}
	viewportState := vk.PipelineViewportStateCreateInfo{
		SType:         vk.StructureTypePipelineViewportStateCreateInfo,
		ViewportCount: 1,
		PViewports:    viewports,
		ScissorCount:  1,
		PScissors:     scissors,
	}

	// Phase 4: specify multisample state
	//					color blend state
	//					rasterizer state

	sampleMask := []vk.SampleMask{vk.SampleMask(vk.MaxUint32)}
	multisampleState := vk.PipelineMultisampleStateCreateInfo{
		SType:                vk.StructureTypePipelineMultisampleStateCreateInfo,
		RasterizationSamples: vk.SampleCount1Bit,
		SampleShadingEnable:  vk.False,
		PSampleMask:          sampleMask,
	}
	attachmentStates := []vk.PipelineColorBlendAttachmentState{{
		ColorWriteMask: vk.ColorComponentFlags(
			vk.ColorComponentRBit | vk.ColorComponentGBit |
				vk.ColorComponentBBit | vk.ColorComponentABit,
		),
		BlendEnable: vk.False,
	}}
	colorBlendState := vk.PipelineColorBlendStateCreateInfo{
		SType:           vk.StructureTypePipelineColorBlendStateCreateInfo,
		LogicOpEnable:   vk.False,
		LogicOp:         vk.LogicOpCopy,
		AttachmentCount: 1,
		PAttachments:    attachmentStates,
	}
	rasterState := vk.PipelineRasterizationStateCreateInfo{
		SType:                   vk.StructureTypePipelineRasterizationStateCreateInfo,
		DepthClampEnable:        vk.False,
		RasterizerDiscardEnable: vk.False,
		PolygonMode:             vk.PolygonModeFill,
		CullMode:                vk.CullModeFlags(vk.CullModeNone),
		FrontFace:               vk.FrontFaceClockwise,
		DepthBiasEnable:         vk.False,
		LineWidth:               1,
	}

	// Phase 5: specify input assembly state
	//					vertex input state and attributes

	inputAssemblyState := vk.PipelineInputAssemblyStateCreateInfo{
		SType:                  vk.StructureTypePipelineInputAssemblyStateCreateInfo,
		Topology:               vk.PrimitiveTopologyTriangleList,
		PrimitiveRestartEnable: vk.True,
	}
	vertexInputBindings := []vk.VertexInputBindingDescription{{
		Binding:   0,
		Stride:    3 * 4, // 4 = sizeof(float32)
		InputRate: vk.VertexInputRateVertex,
	}}
	vertexInputAttributes := []vk.VertexInputAttributeDescription{{
		Binding:  0,
		Location: 0,
		Format:   vk.FormatR32g32b32Sfloat,
		Offset:   0,
	}}
	vertexInputState := vk.PipelineVertexInputStateCreateInfo{
		SType: vk.StructureTypePipelineVertexInputStateCreateInfo,
		VertexBindingDescriptionCount:   1,
		PVertexBindingDescriptions:      vertexInputBindings,
		VertexAttributeDescriptionCount: 1,
		PVertexAttributeDescriptions:    vertexInputAttributes,
	}

	// Phase 5: vk.CreatePipelineCache
	//			vk.CreateGraphicsPipelines

	pipelineCacheInfo := vk.PipelineCacheCreateInfo{
		SType: vk.StructureTypePipelineCacheCreateInfo,
	}
	err = vk.Error(vk.CreatePipelineCache(device, &pipelineCacheInfo, nil, &gfxPipeline.cache))
	if err != nil {
		err = fmt.Errorf("vk.CreatePipelineCache failed with %s", err)
		return gfxPipeline, err
	}
	pipelineCreateInfos := []vk.GraphicsPipelineCreateInfo{{
		SType:               vk.StructureTypeGraphicsPipelineCreateInfo,
		StageCount:          2, // vert + frag
		PStages:             shaderStages,
		PVertexInputState:   &vertexInputState,
		PInputAssemblyState: &inputAssemblyState,
		PViewportState:      &viewportState,
		PRasterizationState: &rasterState,
		PMultisampleState:   &multisampleState,
		PColorBlendState:    &colorBlendState,
		PDynamicState:       &dynamicState,
		Layout:              gfxPipeline.layout,
		RenderPass:          renderPass,
	}}
	pipelines := make([]vk.Pipeline, 1)
	err = vk.Error(vk.CreateGraphicsPipelines(device,
		gfxPipeline.cache, 1, pipelineCreateInfos, nil, pipelines))
	if err != nil {
		err = fmt.Errorf("vk.CreateGraphicsPipelines failed with %s", err)
		return gfxPipeline, err
	}
	gfxPipeline.pipeline = pipelines[0]
	gfxPipeline.device = device
	return gfxPipeline, nil
}

func (gfx *VulkanGfxPipelineInfo) Destroy() {
	if gfx == nil {
		return
	}
	vk.DestroyPipeline(gfx.device, gfx.pipeline, nil)
	vk.DestroyPipelineCache(gfx.device, gfx.cache, nil)
	vk.DestroyPipelineLayout(gfx.device, gfx.layout, nil)
}

func (s *VulkanSwapchainInfo) Destroy() {
	for i := uint32(0); i < s.DefaultSwapchainLen(); i++ {
		vk.DestroyFramebuffer(s.device, s.framebuffers[i], nil)
		vk.DestroyImageView(s.device, s.displayViews[i], nil)
	}
	s.framebuffers = nil
	s.displayViews = nil
	for i := range s.swapchains {
		vk.DestroySwapchain(s.device, s.swapchains[i], nil)
	}
}

func DestroyInOrder(v *VulkanDeviceInfo, s *VulkanSwapchainInfo,
	r *VulkanRenderInfo, b *VulkanBufferInfo, gfx *VulkanGfxPipelineInfo) {

	vk.FreeCommandBuffers(v.device, r.cmdPool, uint32(len(r.cmdBuffers)), r.cmdBuffers)
	r.cmdBuffers = nil

	vk.DestroyCommandPool(v.device, r.cmdPool, nil)
	vk.DestroyRenderPass(v.device, r.renderPass, nil)

	s.Destroy()
	gfx.Destroy()
	b.Destroy()
	vk.DestroyDevice(v.device, nil)
	if v.dbg != vk.NullHandle {
		vk.DestroyDebugReportCallback(v.instance, v.dbg, nil)
	}
	vk.DestroyInstance(v.instance, nil)
}
