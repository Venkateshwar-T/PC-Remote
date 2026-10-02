//go:build windows

package encoder

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
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modMFPlat = windows.NewLazySystemDLL("mfplat.dll")

	procCoInitializeEx       = modOle32.NewProc("CoInitializeEx")
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
		Data1: 0x48eba18e, Data2: 0xf827, Data3: 0x4970,
		Data4: [8]byte{0xb4, 0x50, 0xcb, 0x99, 0xaa, 0x15, 0x22, 0xd7},
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
		Data1: 0xe272446c, Data2: 0xe767, Data3: 0x4972,
		Data4: [8]byte{0xb0, 0x12, 0x18, 0xd5, 0x22, 0x6f, 0x0e, 0x39},
	}
	guidMFMTMPEG2Profile = windows.GUID{
		Data1: 0xad76a80b, Data2: 0x2d5c, Data3: 0x4e0b,
		Data4: [8]byte{0xb3, 0x75, 0x64, 0xe5, 0x20, 0x13, 0x70, 0x36},
	}
)

// H.264 profile (eAVEncH264VProfile). Main profile is broadly compatible with
// browsers while giving better compression than Baseline.
const eAVEncH264VProfile_Main = 77

const (
	CLSCTX_INPROC_SERVER = 1
	COINIT_MULTITHREADED = 0

	MF_VERSION            = 0x00020070
	MFSTARTUP_NOSOCKET    = 1
	MFVideoInterlace_Prog = 2

	MFT_MESSAGE_COMMAND_FLUSH          = 0x00000000
	MFT_MESSAGE_NOTIFY_BEGIN_STREAMING = 0x10000000
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
//	 3 GetStreamLimits         14 GetInputAvailableType
//	 4 GetStreamCount          15 SetInputType
//	 5 GetStreamIDs            16 SetOutputType
//	 6 GetInputStreamInfo      17 GetInputCurrentType
//	 7 GetOutputStreamInfo     18 GetOutputCurrentType
//	 8 GetAttributes           19 GetInputStatus
//	 9 GetInputStreamAttributes 20 GetOutputStatus
//	10 GetOutputStreamAttributes 21 SetOutputBounds
//	11 DeleteInputStream      22 ProcessEvent
//	12 AddInputStreams        23 ProcessMessage
//	13 GetOutputAvailableType 24 ProcessInput
//	                          25 ProcessOutput
const (
	vtIMFTransformSetInputType   = 15
	vtIMFTransformSetOutputType  = 16
	vtIMFTransformProcessMessage = 23
	vtIMFTransformProcessInput   = 24
	vtIMFTransformProcessOutput  = 25
)

// IMFMediaBuffer, in declaration order (own methods start at 3):
// 3 Lock, 4 Unlock, 5 GetCurrentLength, 6 SetCurrentLength, ...
const (
	vtIMFMediaBufferLock             = 3
	vtIMFMediaBufferUnlock           = 4
	vtIMFMediaBufferSetCurrentLength = 6
)

// IMFSample, in declaration order (own methods start at 3):
// 3 GetSampleFlags, ... 37 SetSampleTime, 39 SetSampleDuration, 42 AddBuffer
const (
	vtIMFSampleSetSampleTime     = 37
	vtIMFSampleSetSampleDuration = 39
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

	// Create Output Media Type (H.264)
	var pOutputType uintptr
	r, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&pOutputType)))
	if int32(r) < 0 {
		return fmt.Errorf("MFCreateMediaType failed: 0x%08X", r)
	}
	defer comRelease(pOutputType)

	// Set output type attributes
	setGUID(pOutputType, guidMFMTMajorType, guidMFMediaTypeVideo)
	setGUID(pOutputType, guidMFMTSubtype, guidMFVideoFormatH264)
	setUINT64(pOutputType, guidMFMTFrameSize, packUINT64(uint32(width), uint32(height)))
	setUINT64(pOutputType, guidMFMTFrameRate, packUINT64(uint32(fps), 1))
	setUINT32(pOutputType, guidMFMTAvgBitrate, uint32(bitrate))
	setUINT32(pOutputType, guidMFMTInterlaceMode, MFVideoInterlace_Prog)
	// The H.264 encoder MFT conventionally expects an explicit profile.
	setUINT32(pOutputType, guidMFMTMPEG2Profile, eAVEncH264VProfile_Main)

	// IMFTransform::SetOutputType (vtable index 16)
	r = comCall(pMFT, vtIMFTransformSetOutputType, 0, pOutputType, 0)
	if int32(r) < 0 {
		return fmt.Errorf("IMFTransform::SetOutputType(H264) failed: 0x%08X", r)
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

	// IMFTransform::SetInputType (vtable index 15)
	r = comCall(pMFT, vtIMFTransformSetInputType, 0, pInputType, 0)
	if int32(r) < 0 {
		return fmt.Errorf("IMFTransform::SetInputType(NV12) failed: 0x%08X", r)
	}

	// Notify MFT to begin streaming: IMFTransform::ProcessMessage (vtable index 23)
	comCall(pMFT, vtIMFTransformProcessMessage, MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)

	e.width = width
	e.height = height
	e.fps = fps
	e.bitrate = bitrate
	e.nv12Buffer = make([]byte, (width*height*3)/2)
	e.outputBufCap = uint32(width * height) // Bounded output buffer capacity
	e.sampleCount = 0
	e.keyframeReq = true
	e.initialized = true

	log.Printf("[Encoder] Initialized Windows Media Foundation H.264 MFT (%dx%d @ %dfps, %d bps)", width, height, fps, bitrate)
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

	// 5. Prepare Output Sample
	var pOutputBuf uintptr
	procMFCreateMemoryBuffer.Call(uintptr(e.outputBufCap), uintptr(unsafe.Pointer(&pOutputBuf)))
	defer comRelease(pOutputBuf)

	var pOutputSample uintptr
	procMFCreateSample.Call(uintptr(unsafe.Pointer(&pOutputSample)))
	defer comRelease(pOutputSample)

	comCall(pOutputSample, vtIMFSampleAddBuffer, pOutputBuf)

	var outputBuffer mftOutputDataBuffer
	outputBuffer.pSample = pOutputSample

	var status uint32
	// IMFTransform::ProcessOutput (vtable index 25)
	r = comCall(e.mft, vtIMFTransformProcessOutput, 0, 1, uintptr(unsafe.Pointer(&outputBuffer)), uintptr(unsafe.Pointer(&status)))

	const MF_E_TRANSFORM_NEED_MORE_INPUT = 0xC00D6D9F
	if uint32(r) == MF_E_TRANSFORM_NEED_MORE_INPUT {
		return nil, false, nil // Normal encoder buffering
	}
	if int32(r) < 0 {
		return nil, false, fmt.Errorf("IMFTransform::ProcessOutput failed: 0x%08X", r)
	}

	// 6. Extract Encoded Data
	// IMFMediaBuffer::Lock
	var pOutData uintptr
	var outMax, outLen uint32
	comCall(pOutputBuf, vtIMFMediaBufferLock, uintptr(unsafe.Pointer(&pOutData)), uintptr(unsafe.Pointer(&outMax)), uintptr(unsafe.Pointer(&outLen)))

	outBytes := make([]byte, outLen)
	if outLen > 0 {
		copy(outBytes, unsafe.Slice((*byte)(unsafe.Pointer(pOutData)), outLen))
	}
	comCall(pOutputBuf, vtIMFMediaBufferUnlock)

	// 7. Split into Annex-B NALUs
	nalus := SplitAnnexBNALUs(outBytes)

	// Check if this frame is a keyframe (IDR slice = NAL type 5)
	isKeyframe := false
	for _, nalu := range nalus {
		if len(nalu) > 0 {
			nalType := nalu[0] & 0x1F
			if nalType == 5 || nalType == 7 { // IDR or SPS
				isKeyframe = true
				break
			}
		}
	}

	e.sampleCount++
	return nalus, isKeyframe, nil
}

// Close releases the Media Foundation transform and COM runtime.
func (e *MFTEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cleanupLocked()
	return nil
}

func (e *MFTEncoder) cleanupLocked() {
	if e.mft != 0 {
		comRelease(e.mft)
		e.mft = 0
	}
	if e.initialized {
		procMFShutdown.Call()
		e.initialized = false
	}
}

// IMFAttributes, in declaration order (own methods start at 3):
// 3 GetItem, 4 GetItemType, 5 CompareItem, 6 Compare, 7 GetUINT32, 8 GetUINT64,
// 9 GetDouble, 10 GetGUID, 11 GetStringLength, ..., 17 GetUnknown, 18 SetItem,
// 19 DeleteItem, 20 SetUnknown, 21 SetUINT32, 22 SetUINT64, 23 SetDouble,
// 24 SetGUID, 25 SetString
const (
	vtIMFAttributesSetUINT32 = 21
	vtIMFAttributesSetUINT64 = 22
	vtIMFAttributesSetGUID   = 24
)

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
