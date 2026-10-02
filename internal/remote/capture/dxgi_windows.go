//go:build windows

package capture

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modD3D11 = windows.NewLazySystemDLL("d3d11.dll")
	modDXGI  = windows.NewLazySystemDLL("dxgi.dll")

	procD3D11CreateDevice = modD3D11.NewProc("D3D11CreateDevice")

	// GUIDs
	iidIDXGIDevice = windows.GUID{
		Data1: 0x54ec77fa, Data2: 0x1377, Data3: 0x44e6,
		Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c},
	}
	iidIDXGIOutput1 = windows.GUID{
		Data1: 0x00cddea8, Data2: 0x939b, Data3: 0x4b83,
		Data4: [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc},
	}
	iidID3D11Texture2D = windows.GUID{
		Data1: 0x6f15aff2, Data2: 0xd20e, Data3: 0x42fa,
		Data4: [8]byte{0x9e, 0x2e, 0x57, 0x30, 0xd0, 0x80, 0x0a, 0x23},
	}
)

const (
	D3D_DRIVER_TYPE_UNKNOWN   = 0
	D3D_DRIVER_TYPE_HARDWARE  = 1
	D3D_DRIVER_TYPE_REFERENCE = 2
	D3D_DRIVER_TYPE_NULL      = 3
	D3D_DRIVER_TYPE_SOFTWARE  = 4
	D3D_DRIVER_TYPE_WARP      = 5

	D3D11_CREATE_DEVICE_BGRA_SUPPORT = 0x0020
	D3D11_SDK_VERSION                = 7

	DXGI_FORMAT_B8G8R8A8_UNORM = 87

	D3D11_USAGE_STAGING    = 3
	D3D11_CPU_ACCESS_READ  = 0x00020000
	D3D11_MAP_READ         = 1

	DXGI_ERROR_WAIT_TIMEOUT = 0x887A0027
	DXGI_ERROR_ACCESS_LOST  = 0x887A0026
)

type dxgiOutduplPointerPosition struct {
	X       int32
	Y       int32
	Visible int32
}

type dxgiOutduplFrameInfo struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	TotalMetadataBufferSize   uint32
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerPosition           dxgiOutduplPointerPosition
	Padding                   uint32
}

type d3d11Texture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     struct{ Count, Quality uint32 }
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type d3d11MappedSubresource struct {
	PData      uintptr
	RowPitch   uint32
	DepthPitch uint32
}

// DXGICapture implements Desktop Duplication on Windows.
type DXGICapture struct {
	mu           sync.Mutex
	d3d11Device  uintptr
	d3d11Context uintptr
	duplication  uintptr
	stagingTex   uintptr
	width        int
	height       int
	frameAcquired bool
	cachedBuffer []byte
}

// NewDXGICapture creates a new Desktop Duplication capture instance.
func NewDXGICapture() *DXGICapture {
	return &DXGICapture{}
}

// Init sets up Direct3D 11 device and duplicates the primary display output.
func (c *DXGICapture) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.initLocked()
}

func (c *DXGICapture) initLocked() error {
	c.cleanupLocked()

	// 1. Create D3D11 Device with hardware acceleration
	var pDevice, pContext uintptr
	var featureLevel uint32
	featureLevels := []uint32{0xb000, 0xa100, 0xa000, 0x9300} // 11.0, 10.1, 10.0, 9.3

	ret, _, _ := procD3D11CreateDevice.Call(
		0,                         // pAdapter: default
		D3D_DRIVER_TYPE_HARDWARE, // DriverType
		0,                         // Software module
		D3D11_CREATE_DEVICE_BGRA_SUPPORT,
		uintptr(unsafe.Pointer(&featureLevels[0])),
		uintptr(len(featureLevels)),
		D3D11_SDK_VERSION,
		uintptr(unsafe.Pointer(&pDevice)),
		uintptr(unsafe.Pointer(&featureLevel)),
		uintptr(unsafe.Pointer(&pContext)),
	)
	if int32(ret) < 0 {
		// Hardware failed; try WARP software driver
		ret, _, _ = procD3D11CreateDevice.Call(
			0,
			D3D_DRIVER_TYPE_WARP,
			0,
			D3D11_CREATE_DEVICE_BGRA_SUPPORT,
			uintptr(unsafe.Pointer(&featureLevels[0])),
			uintptr(len(featureLevels)),
			D3D11_SDK_VERSION,
			uintptr(unsafe.Pointer(&pDevice)),
			uintptr(unsafe.Pointer(&featureLevel)),
			uintptr(unsafe.Pointer(&pContext)),
		)
		if int32(ret) < 0 {
			return fmt.Errorf("D3D11CreateDevice failed: HRESULT 0x%08X", ret)
		}
	}

	c.d3d11Device = pDevice
	c.d3d11Context = pContext

	// 2. Query IDXGIDevice from ID3D11Device
	var pDXGIDevice uintptr
	ret = comCall(pDevice, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&pDXGIDevice)))
	if int32(ret) < 0 {
		return fmt.Errorf("QueryInterface(IDXGIDevice) failed: 0x%08X", ret)
	}
	defer comRelease(pDXGIDevice)

	// 3. Get IDXGIAdapter: IDXGIDevice::GetAdapter (vtable index 7)
	var pDXGIAdapter uintptr
	ret = comCall(pDXGIDevice, 7, uintptr(unsafe.Pointer(&pDXGIAdapter)))
	if int32(ret) < 0 {
		return fmt.Errorf("IDXGIDevice::GetAdapter failed: 0x%08X", ret)
	}
	defer comRelease(pDXGIAdapter)

	// 4. Enum primary output: IDXGIAdapter::EnumOutputs(0) (vtable index 7)
	var pDXGIOutput uintptr
	ret = comCall(pDXGIAdapter, 7, 0, uintptr(unsafe.Pointer(&pDXGIOutput)))
	if int32(ret) < 0 {
		return fmt.Errorf("IDXGIAdapter::EnumOutputs(0) failed: 0x%08X", ret)
	}
	defer comRelease(pDXGIOutput)

	// 5. Query IDXGIOutput1
	var pDXGIOutput1 uintptr
	ret = comCall(pDXGIOutput, 0, uintptr(unsafe.Pointer(&iidIDXGIOutput1)), uintptr(unsafe.Pointer(&pDXGIOutput1)))
	if int32(ret) < 0 {
		return fmt.Errorf("QueryInterface(IDXGIOutput1) failed: 0x%08X", ret)
	}
	defer comRelease(pDXGIOutput1)

	// 6. Duplicate Output: IDXGIOutput1::DuplicateOutput (vtable index 18)
	var pDuplication uintptr
	ret = comCall(pDXGIOutput1, 18, pDevice, uintptr(unsafe.Pointer(&pDuplication)))
	if int32(ret) < 0 {
		return fmt.Errorf("IDXGIOutput1::DuplicateOutput failed: 0x%08X", ret)
	}
	c.duplication = pDuplication

	// 7. Get display dimensions: IDXGIOutputDuplication::GetDesc (vtable index 7)
	type dxgiOutduplDesc struct {
		ModeDesc                   struct{ Width, Height, RefreshRateNumerator, RefreshRateDenominator, Format, ScanlineOrdering, Scaling uint32 }
		Rotation                   uint32
		DesktopImageInSystemMemory int32
	}
	var desc dxgiOutduplDesc
	ret = comCall(pDuplication, 7, uintptr(unsafe.Pointer(&desc)))
	if int32(ret) >= 0 {
		c.width = int(desc.ModeDesc.Width)
		c.height = int(desc.ModeDesc.Height)
	} else {
		c.width = 1920
		c.height = 1080
	}

	// 8. Create staging texture for CPU mapping
	stagingDesc := d3d11Texture2DDesc{
		Width:          uint32(c.width),
		Height:         uint32(c.height),
		MipLevels:      1,
		ArraySize:      1,
		Format:         DXGI_FORMAT_B8G8R8A8_UNORM,
		SampleDesc:     struct{ Count, Quality uint32 }{1, 0},
		Usage:          D3D11_USAGE_STAGING,
		CPUAccessFlags: D3D11_CPU_ACCESS_READ,
	}
	var pStagingTex uintptr
	// ID3D11Device::CreateTexture2D (vtable index 5)
	ret = comCall(pDevice, 5, uintptr(unsafe.Pointer(&stagingDesc)), 0, uintptr(unsafe.Pointer(&pStagingTex)))
	if int32(ret) < 0 {
		return fmt.Errorf("CreateTexture2D (staging) failed: 0x%08X", ret)
	}
	c.stagingTex = pStagingTex

	// Pre-allocate contiguous RGBA buffer
	c.cachedBuffer = make([]byte, c.width*c.height*4)

	log.Printf("[DXGI] Initialized Desktop Duplication (%dx%d)", c.width, c.height)
	return nil
}

// AcquireFrame acquires the next screen frame from the Desktop Duplication API.
func (c *DXGICapture) AcquireFrame(timeoutMs uint32) (*CapturedFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.duplication == 0 {
		if err := c.initLocked(); err != nil {
			return nil, err
		}
	}

	if c.frameAcquired {
		// Release previous frame before acquiring next
		// IDXGIOutputDuplication::ReleaseFrame (vtable index 14)
		comCall(c.duplication, 14)
		c.frameAcquired = false
	}

	var frameInfo dxgiOutduplFrameInfo
	var pResource uintptr

	// IDXGIOutputDuplication::AcquireNextFrame (vtable index 8)
	ret := comCall(
		c.duplication,
		8,
		uintptr(timeoutMs),
		uintptr(unsafe.Pointer(&frameInfo)),
		uintptr(unsafe.Pointer(&pResource)),
	)

	if uint32(ret) == DXGI_ERROR_WAIT_TIMEOUT {
		return nil, ErrTimeout
	}
	if uint32(ret) == DXGI_ERROR_ACCESS_LOST {
		log.Println("[DXGI] Duplication access lost, reinitializing...")
		c.cleanupLocked()
		return nil, ErrAccessLost
	}
	if int32(ret) < 0 {
		return nil, fmt.Errorf("AcquireNextFrame failed: 0x%08X", ret)
	}

	c.frameAcquired = true
	defer comRelease(pResource)

	// Query ID3D11Texture2D from resource
	var pDesktopTex uintptr
	ret = comCall(pResource, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&pDesktopTex)))
	if int32(ret) < 0 {
		return nil, fmt.Errorf("QueryInterface(ID3D11Texture2D) failed: 0x%08X", ret)
	}
	defer comRelease(pDesktopTex)

	// Copy desktop texture to staging texture: ID3D11DeviceContext::CopyResource (vtable index 47)
	comCall(c.d3d11Context, 47, c.stagingTex, pDesktopTex)

	// Map staging texture for reading: ID3D11DeviceContext::Map (vtable index 14)
	var mapped d3d11MappedSubresource
	ret = comCall(c.d3d11Context, 14, c.stagingTex, 0, D3D11_MAP_READ, 0, uintptr(unsafe.Pointer(&mapped)))
	if int32(ret) < 0 {
		return nil, fmt.Errorf("ID3D11DeviceContext::Map failed: 0x%08X", ret)
	}

	// Copy row-by-row respecting RowPitch into contiguous BGRA buffer
	srcPtr := mapped.PData
	rowSize := c.width * 4
	rowPitch := int(mapped.RowPitch)

	for y := 0; y < c.height; y++ {
		srcRow := unsafe.Slice((*byte)(unsafe.Pointer(srcPtr+uintptr(y*rowPitch))), rowSize)
		copy(c.cachedBuffer[y*rowSize:(y+1)*rowSize], srcRow)
	}

	// Unmap: ID3D11DeviceContext::Unmap (vtable index 15)
	comCall(c.d3d11Context, 15, c.stagingTex, 0)

	frame := &CapturedFrame{
		Width:     c.width,
		Height:    c.height,
		Data:      c.cachedBuffer,
		Stride:    rowSize,
		CursorX:   frameInfo.PointerPosition.X,
		CursorY:   frameInfo.PointerPosition.Y,
		CursorVis: frameInfo.PointerPosition.Visible != 0,
		Timestamp: time.Now(),
	}

	return frame, nil
}

// ReleaseFrame releases the acquired DXGI duplication frame lock.
func (c *DXGICapture) ReleaseFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frameAcquired && c.duplication != 0 {
		comCall(c.duplication, 14)
		c.frameAcquired = false
	}
}

// Dimensions returns the primary monitor resolution.
func (c *DXGICapture) Dimensions() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width, c.height
}

// Close frees all D3D11 and DXGI resources.
func (c *DXGICapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked()
	return nil
}

func (c *DXGICapture) cleanupLocked() {
	if c.frameAcquired && c.duplication != 0 {
		comCall(c.duplication, 14)
		c.frameAcquired = false
	}
	if c.stagingTex != 0 {
		comRelease(c.stagingTex)
		c.stagingTex = 0
	}
	if c.duplication != 0 {
		comRelease(c.duplication)
		c.duplication = 0
	}
	if c.d3d11Context != 0 {
		comRelease(c.d3d11Context)
		c.d3d11Context = 0
	}
	if c.d3d11Device != 0 {
		comRelease(c.d3d11Device)
		c.d3d11Device = 0
	}
}

func comCall(obj uintptr, vtableIdx int, args ...uintptr) uintptr {
	if obj == 0 {
		return 0x80004005 // E_FAIL
	}
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	method := *(*uintptr)(unsafe.Pointer(vtable + uintptr(vtableIdx)*unsafe.Sizeof(uintptr(0))))

	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, obj)
	callArgs = append(callArgs, args...)

	r1, _, _ := syscall.SyscallN(method, callArgs...)
	return r1
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2) // IUnknown::Release is always vtable index 2
	}
}
