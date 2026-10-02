//go:build windows

package encoder

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modMFPlat = windows.NewLazySystemDLL("mfplat.dll")

	procCoInitializeEx       = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize     = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance     = modOle32.NewProc("CoCreateInstance")
	procMFStartup            = modMFPlat.NewProc("MFStartup")
	procMFShutdown           = modMFPlat.NewProc("MFShutdown")
	procMFCreateMediaType    = modMFPlat.NewProc("MFCreateMediaType")
	procMFCreateSample       = modMFPlat.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer = modMFPlat.NewProc("MFCreateMemoryBuffer")

	// GUIDs
	clsidCMSH264EncoderMFT = windows.GUID{
		Data1: 0x6ca50344, Data2: 0x051a, Data3: 0x4ded,
		Data4: [8]byte{0x97, 0x79, 0xa4, 0x33, 0x05, 0x16, 0x5e, 0x35},
	}
	iidIMFTransform = windows.GUID{
		Data1: 0xbf94c121, Data2: 0x5b05, Data3: 0x4e6f,
		Data4: [8]byte{0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d},
	}
	iidICodecAPI = windows.GUID{
		Data1: 0x901db4c7, Data2: 0x31ce, Data3: 0x41a2,
		Data4: [8]byte{0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda},
	}
	guidMFMediaTypeVideo = windows.GUID{
		Data1: 0x73646976, Data2: 0x0000, Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71},
	}
	guidMFVideoFormatH264 = windows.GUID{
		Data1: 0x34363248, Data2: 0x0000, Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71},
	}
	guidMFVideoFormatNV12 = windows.GUID{
		Data1: 0x3231564e, Data2: 0x0000, Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71},
	}
	guidMFMTMajorType = windows.GUID{
		Data1: 0x48eba18e, Data2: 0xf8c9, Data3: 0x4687,
		Data4: [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f},
	}
	guidMFMTSubtype = windows.GUID{
		Data1: 0xf7e34c9a, Data2: 0x42e8, Data3: 0x4714,
		Data4: [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5},
	}
	guidMFMTFrameSize = windows.GUID{
		Data1: 0x1652c33d, Data2: 0xd6b2, Data3: 0x4012,
		Data4: [8]byte{0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d},
	}
	guidMFMTFrameRate = windows.GUID{
		Data1: 0xc459a2e8, Data2: 0x3d2c, Data3: 0x4e44,
		Data4: [8]byte{0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0},
	}
	guidMFMTAvgBitrate = windows.GUID{
		Data1: 0x20332624, Data2: 0xfb0d, Data3: 0x4d9e,
		Data4: [8]byte{0xbd, 0x0d, 0xcb, 0xf6, 0x78, 0x6c, 0x10, 0x2e},
	}
	guidMFMTInterlaceMode = windows.GUID{
		Data1: 0xe2724bb8, Data2: 0xe676, Data3: 0x4806,
		Data4: [8]byte{0xb4, 0xb2, 0xa8, 0xd6, 0xef, 0xb4, 0x4c, 0xcd},
	}
	guidMFMTMPEG2Profile = windows.GUID{
		Data1: 0xad76a80b, Data2: 0x2d5c, Data3: 0x4e0b,
		Data4: [8]byte{0xb3, 0x75, 0x64, 0xe5, 0x20, 0x13, 0x70, 0x36},
	}
	guidMFMTPixelAspectRatio = windows.GUID{
		Data1: 0xc6376a1e, Data2: 0x8d0a, Data3: 0x4027,
		Data4: [8]byte{0xbe, 0x45, 0x6d, 0x9a, 0x0a, 0xd3, 0x9b, 0xb6},
	}
	guidAVLowLatencyMode = windows.GUID{
		Data1: 0x9c27891a, Data2: 0xed7a, Data3: 0x40e1,
		Data4: [8]byte{0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee},
	}
	guidAVEncVideoForceKeyFrame = windows.GUID{
		Data1: 0x398c1b98, Data2: 0x8353, Data3: 0x475a,
		Data4: [8]byte{0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45},
	}
	guidMFSampleExtensionCleanPoint = windows.GUID{
		Data1: 0x9cdf01d8, Data2: 0xa0f0, Data3: 0x43ba,
		Data4: [8]byte{0xb0, 0x77, 0xea, 0xa0, 0x6c, 0xbd, 0x72, 0x8a},
	}
)

// H.264 profiles (eAVEncH264VProfile from codecapi.h).
const (
	eAVEncH264VProfile_Base = 66
	eAVEncH264VProfile_Main = 77
	eAVEncH264VProfile_High = 100
)

const (
	CLSCTX_INPROC_SERVER = 1
	COINIT_MULTITHREADED = 0

	MF_VERSION            = 0x00020070
	MFSTARTUP_NOSOCKET    = 1
	MFVideoInterlace_Prog = 2

	MFT_MESSAGE_COMMAND_FLUSH          = 0x00000000
	MFT_MESSAGE_NOTIFY_BEGIN_STREAMING = 0x10000000
	MFT_MESSAGE_NOTIFY_START_OF_STREAM = 0x10000001
)

// COM vtable indices, absolute from IUnknown.
//
// Vtable indices continue from the base interface rather than restarting, so
// IUnknown occupies slots 0-2 (QueryInterface/AddRef/Release) and the
// interface's own methods begin at 3. Getting one of these wrong calls an
// unrelated method with a mismatched signature, which surfaces as a confusing
// HRESULT at runtime (e.g. using 8 for SetOutputType calls GetAttributes, which
// writes through an IMFAttributes** out-parameter and fails with E_POINTER).
//
// IMFTransform, in declaration order (own methods start at 3):
//
//	 3 GetStreamLimits         13 GetInputAvailableType
//	 4 GetStreamCount          14 GetOutputAvailableType
//	 5 GetStreamIDs            15 SetInputType
//	 6 GetInputStreamInfo      16 SetOutputType
//	 7 GetOutputStreamInfo     17 GetInputCurrentType
//	 8 GetAttributes           18 GetOutputCurrentType
//	 9 GetInputStreamAttributes 19 GetInputStatus
//	10 GetOutputStreamAttributes 20 GetOutputStatus
//	11 DeleteInputStream      21 SetOutputBounds
//	12 AddInputStreams        22 ProcessEvent
//	                          23 ProcessMessage
//	                          24 ProcessInput
//	                          25 ProcessOutput
const (
	vtIMFTransformGetOutputStreamInfo    = 7
	vtIMFTransformGetInputAvailableType  = 13
	vtIMFTransformGetOutputAvailableType = 14
	vtIMFTransformSetInputType           = 15
	vtIMFTransformSetOutputType          = 16
	vtIMFTransformProcessMessage         = 23
	vtIMFTransformProcessInput           = 24
	vtIMFTransformProcessOutput          = 25
)

// ICodecAPI (own methods start at 3):
// 3 IsSupported, 4 IsModifiable, 5 GetParameterRange, 6 GetParameterValues,
// 7 GetDefaultValue, 8 GetValue, 9 SetValue
const (
	vtICodecAPISetValue = 9
)

// IMFMediaBuffer, in declaration order (own methods start at 3):
// 3 Lock, 4 Unlock, 5 GetCurrentLength, 6 SetCurrentLength, ...
const (
	vtIMFMediaBufferLock             = 3
	vtIMFMediaBufferUnlock           = 4
	vtIMFMediaBufferSetCurrentLength = 6
)

// IMFSample, in declaration order (own methods start at 33 after IMFAttributes 0-32):
// 33 GetSampleFlags, 34 SetSampleFlags, 35 GetSampleTime, 36 SetSampleTime,
// 37 GetSampleDuration, 38 SetSampleDuration, 39 GetBufferCount, ... 42 AddBuffer
const (
	vtIMFSampleSetSampleTime     = 36
	vtIMFSampleSetSampleDuration = 38
	vtIMFSampleAddBuffer         = 42
)

type mftOutputDataBuffer struct {
	dwStreamID uint32
	pSample    uintptr
	dwStatus   uint32
	pEvents    uintptr
}

// MFTEncoder wraps Windows Media Foundation H.264 Encoder MFT.
type MFTEncoder struct {
	mu           sync.Mutex
	mft          uintptr
	codecAPI     uintptr
	width        int
	height       int
	fps          int
	bitrate      int
	nv12Buffer   []byte
	sampleCount  int64
	keyframeReq  bool
	initialized  bool
	outputBufCap uint32
}

// NewMFTEncoder initializes a Media Foundation H.264 encoder.
func NewMFTEncoder() *MFTEncoder {
	return &MFTEncoder{}
}

// Init configures the Media Foundation H.264 encoder MFT.
func (e *MFTEncoder) Init(width, height, fps, bitrate int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if width <= 0 || height <= 0 || (width&1) != 0 || (height&1) != 0 {
		return ErrInvalidDimensions
	}
	if fps <= 0 {
		fps = 30
	}
	if bitrate <= 0 {
		bitrate = 2500000 // 2.5 Mbps
	}

	e.cleanupLocked()

	// Initialize COM
	procCoInitializeEx.Call(0, COINIT_MULTITHREADED)

	// Initialize Media Foundation
	r, _, _ := procMFStartup.Call(MF_VERSION, MFSTARTUP_NOSOCKET)
	if int32(r) < 0 {
		return fmt.Errorf("MFStartup failed: 0x%08X", r)
	}

	// Create H.264 Encoder MFT
	var pMFT uintptr
	r, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCMSH264EncoderMFT)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIMFTransform)),
		uintptr(unsafe.Pointer(&pMFT)),
	)
	if int32(r) < 0 {
		return fmt.Errorf("CoCreateInstance(CMSH264EncoderMFT) failed: 0x%08X", r)
	}
	e.mft = pMFT

	// Configure ICodecAPI for low-latency mode (slice encoding, no B-frame delay)
	var pCodecAPI uintptr
	rQI := comCall(pMFT, 0, uintptr(unsafe.Pointer(&iidICodecAPI)), uintptr(unsafe.Pointer(&pCodecAPI)))
	if int32(rQI) >= 0 {
		e.codecAPI = pCodecAPI
		v := comVariant{vt: 11, val: -1} // VT_BOOL = 11, VARIANT_TRUE = -1
		comCall(pCodecAPI, vtICodecAPISetValue, uintptr(unsafe.Pointer(&guidAVLowLatencyMode)), uintptr(unsafe.Pointer(&v)))
	}

	// Configure Output Media Type (H.264)
	// Query prototype from MFT if available, otherwise create fresh
	var pOutputType uintptr
	rAvail := comCall(pMFT, vtIMFTransformGetOutputAvailableType, 0, 0, uintptr(unsafe.Pointer(&pOutputType)))
	if int32(rAvail) < 0 {
		r, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&pOutputType)))
		if int32(r) < 0 {
			return fmt.Errorf("MFCreateMediaType failed: 0x%08X", r)
		}
	}
	defer comRelease(pOutputType)

	// Set required H.264 output type attributes
	setGUID(pOutputType, guidMFMTMajorType, guidMFMediaTypeVideo)
	setGUID(pOutputType, guidMFMTSubtype, guidMFVideoFormatH264)
	setUINT64(pOutputType, guidMFMTFrameSize, packUINT64(uint32(width), uint32(height)))
	setUINT64(pOutputType, guidMFMTFrameRate, packUINT64(uint32(fps), 1))
	setUINT32(pOutputType, guidMFMTAvgBitrate, uint32(bitrate))
	setUINT32(pOutputType, guidMFMTInterlaceMode, MFVideoInterlace_Prog)
	setUINT32(pOutputType, guidMFMTMPEG2Profile, eAVEncH264VProfile_Base)
	setUINT64(pOutputType, guidMFMTPixelAspectRatio, packUINT64(1, 1))

	// IMFTransform::SetOutputType (vtable index 16)
	r = comCall(pMFT, vtIMFTransformSetOutputType, 0, pOutputType, 0)
	if int32(r) < 0 {
		return fmt.Errorf("IMFTransform::SetOutputType(H264) failed: 0x%08X %s (attempted: %dx%d @ %dfps, %d bps, profile=%d)",
			r, dumpMFTTypes(pMFT), width, height, fps, bitrate, eAVEncH264VProfile_Base)
	}

	// Verify NV12 input type availability
	nv12Supported := false
	for inIdx := uint32(0); inIdx < 16; inIdx++ {
		var pAvailIn uintptr
		rIn := comCall(pMFT, vtIMFTransformGetInputAvailableType, 0, uintptr(inIdx), uintptr(unsafe.Pointer(&pAvailIn)))
		if int32(rIn) < 0 {
			break
		}
		var sub windows.GUID
		comCall(pAvailIn, vtIMFAttributesGetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&sub)))
		comRelease(pAvailIn)
		if sub == guidMFVideoFormatNV12 {
			nv12Supported = true
			break
		}
	}
	if !nv12Supported {
		return fmt.Errorf("H.264 MFT encoder does not support NV12 input %s", dumpMFTTypes(pMFT))
	}

	// Create Input Media Type (NV12)
	var pInputType uintptr
	r, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&pInputType)))
	if int32(r) < 0 {
		return fmt.Errorf("MFCreateMediaType (input) failed: 0x%08X", r)
	}
	defer comRelease(pInputType)

	setGUID(pInputType, guidMFMTMajorType, guidMFMediaTypeVideo)
	setGUID(pInputType, guidMFMTSubtype, guidMFVideoFormatNV12)
	setUINT64(pInputType, guidMFMTFrameSize, packUINT64(uint32(width), uint32(height)))
	setUINT64(pInputType, guidMFMTFrameRate, packUINT64(uint32(fps), 1))
	setUINT32(pInputType, guidMFMTInterlaceMode, MFVideoInterlace_Prog)
	setUINT64(pInputType, guidMFMTPixelAspectRatio, packUINT64(1, 1))

	// IMFTransform::SetInputType (vtable index 15)
	r = comCall(pMFT, vtIMFTransformSetInputType, 0, pInputType, 0)
	if int32(r) < 0 {
		return fmt.Errorf("IMFTransform::SetInputType(NV12) failed: 0x%08X %s (attempted: %dx%d @ %dfps, format=NV12)",
			r, dumpMFTTypes(pMFT), width, height, fps)
	}

	// Notify MFT to begin streaming: IMFTransform::ProcessMessage (vtable index 23)
	comCall(pMFT, vtIMFTransformProcessMessage, MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)
	comCall(pMFT, vtIMFTransformProcessMessage, MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0)

	// Query required output buffer size: IMFTransform::GetOutputStreamInfo (vtable index 7)
	type mftOutputStreamInfo struct {
		dwFlags     uint32
		cbSize      uint32
		cbAlignment uint32
	}
	var streamInfo mftOutputStreamInfo
	comCall(pMFT, vtIMFTransformGetOutputStreamInfo, 0, uintptr(unsafe.Pointer(&streamInfo)))

	e.width = width
	e.height = height
	e.fps = fps
	e.bitrate = bitrate
	e.nv12Buffer = make([]byte, (width*height*3)/2)
	if streamInfo.cbSize > 0 {
		e.outputBufCap = streamInfo.cbSize
	} else {
		e.outputBufCap = uint32(width * height * 2)
	}
	e.sampleCount = 0
	e.keyframeReq = true
	e.initialized = true

	log.Printf("[Encoder] Initialized Windows Media Foundation H.264 MFT (%dx%d @ %dfps, %d bps, bufCap=%d)",
		width, height, fps, bitrate, e.outputBufCap)
	return nil
}

// RequestKeyFrame instructs the encoder to produce an IDR keyframe on the next frame.
func (e *MFTEncoder) RequestKeyFrame() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keyframeReq = true
}

// Encode compresses a BGRA frame and extracts H.264 NALUs.
func (e *MFTEncoder) Encode(bgra []byte, pts time.Duration) ([][]byte, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized || e.mft == 0 {
		return nil, false, ErrEncoderUninitialized
	}

	// Force keyframe if requested
	if e.keyframeReq {
		if e.codecAPI != 0 {
			vkf := comVariant{vt: 19, val: 1} // VT_UI4 = 19
			comCall(e.codecAPI, vtICodecAPISetValue, uintptr(unsafe.Pointer(&guidAVEncVideoForceKeyFrame)), uintptr(unsafe.Pointer(&vkf)))
		}
		e.keyframeReq = false
	}

	// 1. Convert BGRA to NV12
	stride := e.width * 4
	ConvertBGRAToNV12(bgra, e.width, e.height, stride, e.nv12Buffer)

	// 2. Create Media Foundation Memory Buffer for input
	inputSize := len(e.nv12Buffer)
	var pInputBuf uintptr
	r, _, _ := procMFCreateMemoryBuffer.Call(uintptr(inputSize), uintptr(unsafe.Pointer(&pInputBuf)))
	if int32(r) < 0 {
		return nil, false, fmt.Errorf("MFCreateMemoryBuffer failed: 0x%08X", r)
	}
	defer comRelease(pInputBuf)

	// Lock buffer: IMFMediaBuffer::Lock
	var pDstData uintptr
	var maxLen, curLen uint32
	r = comCall(pInputBuf, vtIMFMediaBufferLock, uintptr(unsafe.Pointer(&pDstData)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))
	if int32(r) < 0 {
		return nil, false, fmt.Errorf("IMFMediaBuffer::Lock failed: 0x%08X", r)
	}

	copy(unsafe.Slice((*byte)(unsafe.Pointer(pDstData)), inputSize), e.nv12Buffer)

	// Unlock: IMFMediaBuffer::Unlock
	comCall(pInputBuf, vtIMFMediaBufferUnlock)
	// SetCurrentLength: IMFMediaBuffer::SetCurrentLength
	comCall(pInputBuf, vtIMFMediaBufferSetCurrentLength, uintptr(inputSize))

	// 3. Create Sample and attach buffer
	var pInputSample uintptr
	procMFCreateSample.Call(uintptr(unsafe.Pointer(&pInputSample)))
	defer comRelease(pInputSample)

	// IMFSample::AddBuffer
	comCall(pInputSample, vtIMFSampleAddBuffer, pInputBuf)

	// IMFSample::SetSampleTime
	frameTimeHNS := pts.Nanoseconds() / 100
	comCall(pInputSample, vtIMFSampleSetSampleTime, uintptr(frameTimeHNS))

	// IMFSample::SetSampleDuration
	frameDurationHNS := int64(time.Second/time.Duration(e.fps)) / 100
	comCall(pInputSample, vtIMFSampleSetSampleDuration, uintptr(frameDurationHNS))

	// 4. Send Sample to MFT: IMFTransform::ProcessInput (vtable index 24)
	r = comCall(e.mft, vtIMFTransformProcessInput, 0, pInputSample, 0)
	if int32(r) < 0 {
		return nil, false, fmt.Errorf("IMFTransform::ProcessInput failed: 0x%08X", r)
	}

	// 5. Drain Output Samples: IMFTransform::ProcessOutput (vtable index 25)
	var allNalus [][]byte
	isKeyframe := false

	const MF_E_TRANSFORM_NEED_MORE_INPUT = 0xC00D6D72

	for {
		var pOutputBuf uintptr
		rBuf, _, _ := procMFCreateMemoryBuffer.Call(uintptr(e.outputBufCap), uintptr(unsafe.Pointer(&pOutputBuf)))
		if int32(rBuf) < 0 {
			break
		}

		var pOutputSample uintptr
		procMFCreateSample.Call(uintptr(unsafe.Pointer(&pOutputSample)))
		comCall(pOutputSample, vtIMFSampleAddBuffer, pOutputBuf)

		var outputBuffer mftOutputDataBuffer
		outputBuffer.pSample = pOutputSample

		var status uint32
		rOut := comCall(e.mft, vtIMFTransformProcessOutput, 0, 1, uintptr(unsafe.Pointer(&outputBuffer)), uintptr(unsafe.Pointer(&status)))

		if uint32(rOut) == MF_E_TRANSFORM_NEED_MORE_INPUT {
			comRelease(pOutputSample)
			comRelease(pOutputBuf)
			break
		}
		if int32(rOut) < 0 {
			comRelease(pOutputSample)
			comRelease(pOutputBuf)
			return nil, false, fmt.Errorf("IMFTransform::ProcessOutput failed: 0x%08X", rOut)
		}

		// 6. Extract Encoded Data
		var pOutData uintptr
		var outMax, outLen uint32
		comCall(pOutputBuf, vtIMFMediaBufferLock, uintptr(unsafe.Pointer(&pOutData)), uintptr(unsafe.Pointer(&outMax)), uintptr(unsafe.Pointer(&outLen)))

		if outLen > 0 {
			outBytes := make([]byte, outLen)
			copy(outBytes, unsafe.Slice((*byte)(unsafe.Pointer(pOutData)), outLen))
			comCall(pOutputBuf, vtIMFMediaBufferUnlock)

			// 7. Split into Annex-B NALUs
			nalus := SplitAnnexBNALUs(outBytes)
			for _, nalu := range nalus {
				if len(nalu) > 0 {
					nalType := nalu[0] & 0x1F
					if nalType == 5 || nalType == 7 { // IDR or SPS
						isKeyframe = true
					}
					allNalus = append(allNalus, nalu)
				}
			}
		} else {
			comCall(pOutputBuf, vtIMFMediaBufferUnlock)
		}

		comRelease(pOutputSample)
		comRelease(pOutputBuf)
	}

	e.sampleCount++
	return allNalus, isKeyframe, nil
}

// Close releases the Media Foundation transform and COM runtime.
func (e *MFTEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cleanupLocked()
	return nil
}

func (e *MFTEncoder) cleanupLocked() {
	if e.codecAPI != 0 {
		comRelease(e.codecAPI)
		e.codecAPI = 0
	}
	if e.mft != 0 {
		comRelease(e.mft)
		e.mft = 0
	}
	if e.initialized {
		procMFShutdown.Call()
		procCoUninitialize.Call()
		e.initialized = false
	}
}

// IMFAttributes, in declaration order (own methods start at 3):
// 3 GetItem, 4 GetItemType, 5 CompareItem, 6 Compare, 7 GetUINT32, 8 GetUINT64,
// 9 GetDouble, 10 GetGUID, 11 GetStringLength, ..., 17 GetUnknown, 18 SetItem,
// 19 DeleteItem, 20 SetUnknown, 21 SetUINT32, 22 SetUINT64, 23 SetDouble,
// 24 SetGUID, 25 SetString
const (
	vtIMFAttributesGetGUID   = 10
	vtIMFAttributesSetUINT32 = 21
	vtIMFAttributesSetUINT64 = 22
	vtIMFAttributesSetGUID   = 24
)

type comVariant struct {
	vt         uint16
	wReserved1 uint16
	wReserved2 uint16
	wReserved3 uint16
	val        int64
	val2       int64
}

func dumpMFTTypes(pMFT uintptr) string {
	var sb strings.Builder
	sb.WriteString("[Available Output Types: ")
	outCount := 0
	for i := uint32(0); i < 16; i++ {
		var pType uintptr
		r := comCall(pMFT, vtIMFTransformGetOutputAvailableType, 0, uintptr(i), uintptr(unsafe.Pointer(&pType)))
		if int32(r) < 0 {
			break
		}
		var sub windows.GUID
		comCall(pType, vtIMFAttributesGetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&sub)))
		comRelease(pType)
		if outCount > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%d:%+v", i, sub))
		outCount++
	}
	sb.WriteString(fmt.Sprintf(" (total %d); Available Input Types: ", outCount))
	inCount := 0
	for i := uint32(0); i < 16; i++ {
		var pType uintptr
		r := comCall(pMFT, vtIMFTransformGetInputAvailableType, 0, uintptr(i), uintptr(unsafe.Pointer(&pType)))
		if int32(r) < 0 {
			break
		}
		var sub windows.GUID
		comCall(pType, vtIMFAttributesGetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&sub)))
		comRelease(pType)
		if inCount > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%d:%+v", i, sub))
		inCount++
	}
	sb.WriteString(fmt.Sprintf(" (total %d)]", inCount))
	return sb.String()
}

func setGUID(pObj uintptr, key windows.GUID, val windows.GUID) {
	// IMFAttributes::SetGUID
	comCall(pObj, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&val)))
}

func setUINT32(pObj uintptr, key windows.GUID, val uint32) {
	// IMFAttributes::SetUINT32
	comCall(pObj, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&key)), uintptr(val))
}

func setUINT64(pObj uintptr, key windows.GUID, val uint64) {
	// IMFAttributes::SetUINT64
	comCall(pObj, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&key)), uintptr(val))
}

func packUINT64(high, low uint32) uint64 {
	return (uint64(high) << 32) | uint64(low)
}

func comCall(obj uintptr, vtableIdx int, args ...uintptr) uintptr {
	if obj == 0 {
		return 0x80004005
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
		comCall(obj, 2)
	}
}
