//go:build windows

package encoder

import (
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestMFT_VTableIndices locks down the absolute COM vtable indices used against
// IMFTransform. Indices continue from IUnknown (slots 0-2), so an off-by-N here
// invokes an unrelated method with a mismatched signature and fails at runtime
// with a misleading HRESULT (e.g. 8 = GetAttributes -> E_POINTER 0x80004003).
func TestMFT_VTableIndices(t *testing.T) {
	// IMFTransform methods in declaration order after the IUnknown base.
	order := []string{
		"GetStreamLimits",           // 3
		"GetStreamCount",            // 4
		"GetStreamIDs",              // 5
		"GetInputStreamInfo",        // 6
		"GetOutputStreamInfo",       // 7
		"GetAttributes",             // 8
		"GetInputStreamAttributes",  // 9
		"GetOutputStreamAttributes", // 10
		"DeleteInputStream",         // 11
		"AddInputStreams",           // 12
		"GetInputAvailableType",     // 13
		"GetOutputAvailableType",    // 14
		"SetInputType",              // 15
		"SetOutputType",             // 16
		"GetInputCurrentType",       // 17
		"GetOutputCurrentType",      // 18
		"GetInputStatus",            // 19
		"GetOutputStatus",           // 20
		"SetOutputBounds",           // 21
		"ProcessEvent",              // 22
		"ProcessMessage",            // 23
		"ProcessInput",              // 24
		"ProcessOutput",             // 25
	}

	indexOf := func(name string) int {
		for i, m := range order {
			if m == name {
				return 3 + i // 3 == IUnknown.QueryInterface, AddRef, Release
			}
		}
		t.Fatalf("method %q not in IMFTransform declaration order", name)
		return -1
	}

	cases := []struct {
		name string
		got  int
		want string
	}{
		{"GetOutputStreamInfo", vtIMFTransformGetOutputStreamInfo, "GetOutputStreamInfo"},
		{"GetInputAvailableType", vtIMFTransformGetInputAvailableType, "GetInputAvailableType"},
		{"GetOutputAvailableType", vtIMFTransformGetOutputAvailableType, "GetOutputAvailableType"},
		{"SetInputType", vtIMFTransformSetInputType, "SetInputType"},
		{"SetOutputType", vtIMFTransformSetOutputType, "SetOutputType"},
		{"ProcessMessage", vtIMFTransformProcessMessage, "ProcessMessage"},
		{"ProcessInput", vtIMFTransformProcessInput, "ProcessInput"},
		{"ProcessOutput", vtIMFTransformProcessOutput, "ProcessOutput"},
	}

	for _, tc := range cases {
		want := indexOf(tc.want)
		if tc.got != want {
			t.Errorf("%s: vtable index = %d, want %d", tc.name, tc.got, want)
		}
	}

	// Verify IMFSample methods (IUnknown 0-2, IMFAttributes 3-32, own methods 33+)
	if vtIMFSampleSetSampleTime != 36 {
		t.Errorf("vtIMFSampleSetSampleTime = %d, want 36", vtIMFSampleSetSampleTime)
	}
	if vtIMFSampleSetSampleDuration != 38 {
		t.Errorf("vtIMFSampleSetSampleDuration = %d, want 38", vtIMFSampleSetSampleDuration)
	}
	if vtIMFSampleAddBuffer != 42 {
		t.Errorf("vtIMFSampleAddBuffer = %d, want 42", vtIMFSampleAddBuffer)
	}

	// Verify IMFMediaBuffer methods (IUnknown 0-2, own methods 3+)
	if vtIMFMediaBufferLock != 3 {
		t.Errorf("vtIMFMediaBufferLock = %d, want 3", vtIMFMediaBufferLock)
	}
	if vtIMFMediaBufferUnlock != 4 {
		t.Errorf("vtIMFMediaBufferUnlock = %d, want 4", vtIMFMediaBufferUnlock)
	}
	if vtIMFMediaBufferSetCurrentLength != 6 {
		t.Errorf("vtIMFMediaBufferSetCurrentLength = %d, want 6", vtIMFMediaBufferSetCurrentLength)
	}

	// Verify ICodecAPI methods (IUnknown 0-2, own methods 3+)
	if vtICodecAPISetValue != 9 {
		t.Errorf("vtICodecAPISetValue = %d, want 9", vtICodecAPISetValue)
	}

	// Verify IMFAttributes methods
	if vtIMFAttributesGetGUID != 10 {
		t.Errorf("vtIMFAttributesGetGUID = %d, want 10", vtIMFAttributesGetGUID)
	}
	if vtIMFAttributesSetUINT32 != 21 {
		t.Errorf("vtIMFAttributesSetUINT32 = %d, want 21", vtIMFAttributesSetUINT32)
	}
	if vtIMFAttributesSetUINT64 != 22 {
		t.Errorf("vtIMFAttributesSetUINT64 = %d, want 22", vtIMFAttributesSetUINT64)
	}
	if vtIMFAttributesSetGUID != 24 {
		t.Errorf("vtIMFAttributesSetGUID = %d, want 24", vtIMFAttributesSetGUID)
	}
}

func TestMFT_GUIDsAgainstWindowsSDK(t *testing.T) {
	tests := []struct {
		name     string
		actual   windows.GUID
		expected string
	}{
		{
			name:     "CLSID_CMSH264EncoderMFT",
			actual:   clsidCMSH264EncoderMFT,
			expected: "{6ca50344-051a-4ded-9779-a43305165e35}",
		},
		{
			name:     "IID_IMFTransform",
			actual:   iidIMFTransform,
			expected: "{bf94c121-5b05-4e6f-8000-ba598961414d}",
		},
		{
			name:     "IID_ICodecAPI",
			actual:   iidICodecAPI,
			expected: "{901db4c7-31ce-41a2-85dc-8fa0bf41b8da}",
		},
		{
			name:     "MFMediaType_Video",
			actual:   guidMFMediaTypeVideo,
			expected: "{73646976-0000-0010-8000-00aa00389b71}",
		},
		{
			name:     "MFVideoFormat_H264",
			actual:   guidMFVideoFormatH264,
			expected: "{34363248-0000-0010-8000-00aa00389b71}",
		},
		{
			name:     "MFVideoFormat_NV12",
			actual:   guidMFVideoFormatNV12,
			expected: "{3231564e-0000-0010-8000-00aa00389b71}",
		},
		{
			name:     "MF_MT_MAJOR_TYPE",
			actual:   guidMFMTMajorType,
			expected: "{48eba18e-f8c9-4687-bf11-0a74c9f96a8f}",
		},
		{
			name:     "MF_MT_SUBTYPE",
			actual:   guidMFMTSubtype,
			expected: "{f7e34c9a-42e8-4714-b74b-cb29d72c35e5}",
		},
		{
			name:     "MF_MT_FRAME_SIZE",
			actual:   guidMFMTFrameSize,
			expected: "{1652c33d-d6b2-4012-b834-72030849a37d}",
		},
		{
			name:     "MF_MT_FRAME_RATE",
			actual:   guidMFMTFrameRate,
			expected: "{c459a2e8-3d2c-4e44-b132-fee5156c7bb0}",
		},
		{
			name:     "MF_MT_AVG_BITRATE",
			actual:   guidMFMTAvgBitrate,
			expected: "{20332624-fb0d-4d9e-bd0d-cbf6786c102e}",
		},
		{
			name:     "MF_MT_INTERLACE_MODE",
			actual:   guidMFMTInterlaceMode,
			expected: "{e2724bb8-e676-4806-b4b2-a8d6efb44ccd}",
		},
		{
			name:     "MF_MT_MPEG2_PROFILE",
			actual:   guidMFMTMPEG2Profile,
			expected: "{ad76a80b-2d5c-4e0b-b375-64e520137036}",
		},
		{
			name:     "MF_MT_PIXEL_ASPECT_RATIO",
			actual:   guidMFMTPixelAspectRatio,
			expected: "{c6376a1e-8d0a-4027-be45-6d9a0ad39bb6}",
		},
		{
			name:     "CODECAPI_AVLowLatencyMode",
			actual:   guidAVLowLatencyMode,
			expected: "{9c27891a-ed7a-40e1-88e8-b22727a024ee}",
		},
		{
			name:     "CODECAPI_AVEncVideoForceKeyFrame",
			actual:   guidAVEncVideoForceKeyFrame,
			expected: "{398c1b98-8353-475a-9ef2-8f265d260345}",
		},
		{
			name:     "MFSampleExtension_CleanPoint",
			actual:   guidMFSampleExtensionCleanPoint,
			expected: "{9cdf01d8-a0f0-43ba-b077-eaa06cbd728a}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectedGUID, err := windows.GUIDFromString(tc.expected)
			if err != nil {
				t.Fatalf("failed to parse expected GUID %s: %v", tc.expected, err)
			}
			if tc.actual != expectedGUID {
				t.Fatalf("%s does not match Windows SDK canonical GUID!\nActual:   %+v\nExpected: %+v (%s)",
					tc.name, tc.actual, expectedGUID, tc.expected)
			}
		})
	}
}

func TestMFT_InspectEncoderLive(t *testing.T) {
	procCoInitializeEx.Call(0, COINIT_MULTITHREADED)
	defer procCoUninitialize.Call()

	r, _, _ := procMFStartup.Call(MF_VERSION, MFSTARTUP_NOSOCKET)
	if int32(r) < 0 {
		t.Fatalf("MFStartup failed: 0x%08X", r)
	}
	defer procMFShutdown.Call()

	var pMFT uintptr
	r, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCMSH264EncoderMFT)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIMFTransform)),
		uintptr(unsafe.Pointer(&pMFT)),
	)
	if int32(r) < 0 {
		t.Fatalf("CoCreateInstance(CMSH264EncoderMFT) failed: 0x%08X", r)
	}
	defer comRelease(pMFT)

	iidICodecAPI := windows.GUID{
		Data1: 0x901db4c7, Data2: 0x31ce, Data3: 0x41a2,
		Data4: [8]byte{0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda},
	}
	var pCodecAPI uintptr
	rQI := comCall(pMFT, 0, uintptr(unsafe.Pointer(&iidICodecAPI)), uintptr(unsafe.Pointer(&pCodecAPI)))
	t.Logf("QueryInterface(ICodecAPI) => 0x%08X", rQI)
	if int32(rQI) >= 0 {
		defer comRelease(pCodecAPI)

		// Test CODECAPI_AVLowLatencyMode
		// STATIC_CODECAPI_AVLowLatencyMode 0x9c27891a, 0xed7a, 0x40e1, 0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee
		guidAVLowLatencyMode := windows.GUID{
			Data1: 0x9c27891a, Data2: 0xed7a, Data3: 0x40e1,
			Data4: [8]byte{0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee},
		}
		// VARIANT: VT_BOOL = 11, VARIANT_TRUE = -1 (0xFFFF)
		type variant struct {
			vt         uint16
			wReserved1 uint16
			wReserved2 uint16
			wReserved3 uint16
			val        int64
			val2       int64
		}
		v := variant{vt: 11, val: -1}
		rSetVal := comCall(pCodecAPI, 9, uintptr(unsafe.Pointer(&guidAVLowLatencyMode)), uintptr(unsafe.Pointer(&v)))
		t.Logf("ICodecAPI::SetValue(AVLowLatencyMode) => 0x%08X", rSetVal)

		// Test CODECAPI_AVEncVideoForceKeyFrame
		// STATIC_CODECAPI_AVEncVideoForceKeyFrame 0x398c1b98, 0x8353, 0x475a, 0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x3, 0x45
		guidAVEncVideoForceKeyFrame := windows.GUID{
			Data1: 0x398c1b98, Data2: 0x8353, Data3: 0x475a,
			Data4: [8]byte{0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45},
		}
		vkf := variant{vt: 19, val: 1} // VT_UI4 = 19
		rSetKF := comCall(pCodecAPI, 9, uintptr(unsafe.Pointer(&guidAVEncVideoForceKeyFrame)), uintptr(unsafe.Pointer(&vkf)))
		t.Logf("ICodecAPI::SetValue(AVEncVideoForceKeyFrame) => 0x%08X", rSetKF)
	}

	// Check stream count (vtable index 4)
	var inStreams, outStreams uint32
	r = comCall(pMFT, 4, uintptr(unsafe.Pointer(&inStreams)), uintptr(unsafe.Pointer(&outStreams)))
	t.Logf("GetStreamCount: ret=0x%08X, inStreams=%d, outStreams=%d", r, inStreams, outStreams)

	// Inspect available output types (vtable index 14)
	t.Log("Enumerating GetOutputAvailableType(0, ...):")
	for idx := uint32(0); idx < 20; idx++ {
		var pType uintptr
		r = comCall(pMFT, 14, 0, uintptr(idx), uintptr(unsafe.Pointer(&pType)))
		if int32(r) < 0 {
			t.Logf("  Output type %d: failed with 0x%08X (end of types or not ready)", idx, r)
			break
		}
		defer comRelease(pType)

		// Get major type and subtype
		var major, sub windows.GUID
		rMajor := comCall(pType, 10, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&major)))
		rSub := comCall(pType, 10, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&sub)))

		var profile, interlace, bitrate uint32
		comCall(pType, 7, uintptr(unsafe.Pointer(&guidMFMTMPEG2Profile)), uintptr(unsafe.Pointer(&profile)))
		comCall(pType, 7, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(unsafe.Pointer(&interlace)))
		comCall(pType, 7, uintptr(unsafe.Pointer(&guidMFMTAvgBitrate)), uintptr(unsafe.Pointer(&bitrate)))

		var frameSize, frameRate uint64
		comCall(pType, 8, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(unsafe.Pointer(&frameSize)))
		comCall(pType, 8, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(unsafe.Pointer(&frameRate)))

		t.Logf("  Output type %d: rMajor=0x%08X major=%+v, rSub=0x%08X sub=%+v, profile=%d, interlace=%d, size=%dx%d, fps=%d/%d, bitrate=%d",
			idx, rMajor, major, rSub, sub, profile, interlace, uint32(frameSize>>32), uint32(frameSize), uint32(frameRate>>32), uint32(frameRate), bitrate)
	}

	// Test creating output type and setting attributes individually
	var pOutputType uintptr
	r, _, _ = procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&pOutputType)))
	t.Logf("MFCreateMediaType: 0x%08X", r)
	defer comRelease(pOutputType)

	r1 := comCall(pOutputType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&guidMFMediaTypeVideo)))
	r2 := comCall(pOutputType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&guidMFVideoFormatH264)))
	r3 := comCall(pOutputType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(packUINT64(1920, 1080)))
	r4 := comCall(pOutputType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(packUINT64(30, 1)))
	r5 := comCall(pOutputType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTAvgBitrate)), uintptr(2500000))
	r6 := comCall(pOutputType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(MFVideoInterlace_Prog))
	r7 := comCall(pOutputType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTMPEG2Profile)), uintptr(eAVEncH264VProfile_Main))
	t.Logf("Set attributes: r1=%08X r2=%08X r3=%08X r4=%08X r5=%08X r6=%08X r7=%08X", r1, r2, r3, r4, r5, r6, r7)

	// Read back attributes from pOutputType
	var readSize, readRate uint64
	var readBitrate, readInterlace, readProfile uint32
	var readMajor, readSub windows.GUID

	rMajGet := comCall(pOutputType, 10, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&readMajor)))
	rSubGet := comCall(pOutputType, 10, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&readSub)))
	rSzGet := comCall(pOutputType, 8, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(unsafe.Pointer(&readSize)))
	rRtGet := comCall(pOutputType, 8, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(unsafe.Pointer(&readRate)))
	rBrGet := comCall(pOutputType, 7, uintptr(unsafe.Pointer(&guidMFMTAvgBitrate)), uintptr(unsafe.Pointer(&readBitrate)))
	rIlGet := comCall(pOutputType, 7, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(unsafe.Pointer(&readInterlace)))
	rPrGet := comCall(pOutputType, 7, uintptr(unsafe.Pointer(&guidMFMTMPEG2Profile)), uintptr(unsafe.Pointer(&readProfile)))

	t.Logf("Readback pOutputType:")
	t.Logf("  Major: 0x%08X (match=%v)", rMajGet, readMajor == guidMFMediaTypeVideo)
	t.Logf("  Sub: 0x%08X (match=%v)", rSubGet, readSub == guidMFVideoFormatH264)
	t.Logf("  Size: 0x%08X (w=%d, h=%d)", rSzGet, uint32(readSize>>32), uint32(readSize))
	t.Logf("  Rate: 0x%08X (num=%d, den=%d)", rRtGet, uint32(readRate>>32), uint32(readRate))
	t.Logf("  Bitrate: 0x%08X (val=%d)", rBrGet, readBitrate)
	t.Logf("  Interlace: 0x%08X (val=%d)", rIlGet, readInterlace)
	t.Logf("  Profile: 0x%08X (val=%d)", rPrGet, readProfile)

	guidMFMTPixelAspectRatio := windows.GUID{
		Data1: 0xc6376a1e,
		Data2: 0x8d0a,
		Data3: 0x4027,
		Data4: [8]byte{0xbe, 0x45, 0x6d, 0x9a, 0x0a, 0xd3, 0x9b, 0xb6},
	}

	testSetOutput := func(name string, pType uintptr) {
		t.Logf("=== Testing SetOutputType with %s ===", name)
		for _, profile := range []uint32{eAVEncH264VProfile_Base, eAVEncH264VProfile_Main, eAVEncH264VProfile_High} {
			for _, setPAR := range []bool{false, true} {
				// Clear or recreate
				var testType uintptr
				procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&testType)))
				// Copy attributes if pType provided
				if pType != 0 {
					comCall(pType, 32, testType) // IMFAttributes::CopyAllItems
				}
				comCall(testType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&guidMFMediaTypeVideo)))
				comCall(testType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&guidMFVideoFormatH264)))
				comCall(testType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(packUINT64(1920, 1080)))
				comCall(testType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(packUINT64(30, 1)))
				comCall(testType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTAvgBitrate)), uintptr(2500000))
				comCall(testType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(MFVideoInterlace_Prog))
				comCall(testType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTMPEG2Profile)), uintptr(profile))
				if setPAR {
					comCall(testType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTPixelAspectRatio)), uintptr(packUINT64(1, 1)))
				}

				rSet := comCall(pMFT, 16, 0, testType, 0)
				t.Logf("  profile=%d setPAR=%v => 0x%08X", profile, setPAR, rSet)
				comRelease(testType)
			}
		}
	}

	testSetOutput("fresh MFCreateMediaType", 0)

	// Step-by-step diagnosis of GetOutputAvailableType(0, 0)
	var pAvail uintptr
	rAvail := comCall(pMFT, 14, 0, 0, uintptr(unsafe.Pointer(&pAvail)))
	t.Logf("GetOutputAvailableType(0, 0) => 0x%08X", rAvail)
	if int32(rAvail) >= 0 {
		defer comRelease(pAvail)

		// Test SetOutputType with pAvail as-is
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 0: SetOutputType(pAvail as-is) => 0x%08X", r)

		// Set MF_MT_MAJOR_TYPE
		s1 := comCall(pAvail, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&guidMFMediaTypeVideo)))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 1: +MAJOR_TYPE (s1=0x%08X) => 0x%08X", s1, r)

		// Set MF_MT_FRAME_SIZE
		s2 := comCall(pAvail, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(packUINT64(1920, 1080)))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 2: +FRAME_SIZE (s2=0x%08X) => 0x%08X", s2, r)

		// Set MF_MT_FRAME_RATE
		s3 := comCall(pAvail, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(packUINT64(30, 1)))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 3: +FRAME_RATE (s3=0x%08X) => 0x%08X", s3, r)

		// Set MF_MT_AVG_BITRATE
		s4 := comCall(pAvail, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTAvgBitrate)), uintptr(2500000))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 4: +AVG_BITRATE (s4=0x%08X) => 0x%08X", s4, r)

		// Set MF_MT_INTERLACE_MODE
		s5 := comCall(pAvail, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(MFVideoInterlace_Prog))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 5: +INTERLACE_MODE (s5=0x%08X) => 0x%08X", s5, r)

		// Set MF_MT_MPEG2_PROFILE
		s6 := comCall(pAvail, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTMPEG2Profile)), uintptr(eAVEncH264VProfile_Base))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 6: +MPEG2_PROFILE(Base) (s6=0x%08X) => 0x%08X", s6, r)

		// Set MF_MT_PIXEL_ASPECT_RATIO
		s7 := comCall(pAvail, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTPixelAspectRatio)), uintptr(packUINT64(1, 1)))
		r = comCall(pMFT, 16, 0, pAvail, 0)
		t.Logf("Step 7: +PIXEL_ASPECT_RATIO (s7=0x%08X) => 0x%08X", s7, r)

		var cnt uint32
		comCall(pAvail, 30, uintptr(unsafe.Pointer(&cnt)))
		t.Logf("pAvail total attribute count: %d", cnt)

		// Now test enumerating available input types (vtable index 13)
		t.Log("Enumerating GetInputAvailableType(0, ...):")
		for inIdx := uint32(0); inIdx < 10; inIdx++ {
			var pInType uintptr
			rIn := comCall(pMFT, 13, 0, uintptr(inIdx), uintptr(unsafe.Pointer(&pInType)))
			if int32(rIn) < 0 {
				t.Logf("  Input type %d: failed with 0x%08X (end of types)", inIdx, rIn)
				break
			}
			defer comRelease(pInType)

			var inMajor, inSub windows.GUID
			comCall(pInType, 10, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&inMajor)))
			comCall(pInType, 10, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&inSub)))
			t.Logf("  Input type %d: major=%+v sub=%+v", inIdx, inMajor, inSub)
		}

		// Now test SetInputType (vtable index 15) with NV12
		var pInType uintptr
		procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&pInType)))
		defer comRelease(pInType)

		comCall(pInType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTMajorType)), uintptr(unsafe.Pointer(&guidMFMediaTypeVideo)))
		comCall(pInType, vtIMFAttributesSetGUID, uintptr(unsafe.Pointer(&guidMFMTSubtype)), uintptr(unsafe.Pointer(&guidMFVideoFormatNV12)))
		comCall(pInType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameSize)), uintptr(packUINT64(1920, 1080)))
		comCall(pInType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTFrameRate)), uintptr(packUINT64(30, 1)))
		comCall(pInType, vtIMFAttributesSetUINT32, uintptr(unsafe.Pointer(&guidMFMTInterlaceMode)), uintptr(MFVideoInterlace_Prog))
		comCall(pInType, vtIMFAttributesSetUINT64, uintptr(unsafe.Pointer(&guidMFMTPixelAspectRatio)), uintptr(packUINT64(1, 1)))

		rSetIn := comCall(pMFT, 15, 0, pInType, 0)
		t.Logf("SetInputType(0, NV12, 0) => 0x%08X", rSetIn)

		// Test streaming messages (vtable index 23: ProcessMessage)
		// MFT_MESSAGE_COMMAND_FLUSH = 0
		// MFT_MESSAGE_NOTIFY_BEGIN_STREAMING = 0x10000000
		// MFT_MESSAGE_NOTIFY_START_OF_STREAM = 0x10000001
		rMsg := comCall(pMFT, 23, 0x10000000, 0)
		t.Logf("ProcessMessage(NOTIFY_BEGIN_STREAMING) => 0x%08X", rMsg)
		rMsgStart := comCall(pMFT, 23, 0x10000001, 0)
		t.Logf("ProcessMessage(NOTIFY_START_OF_STREAM) => 0x%08X", rMsgStart)
	}
}

func TestMFT_EncoderInitAndEncodeLive(t *testing.T) {
	enc := NewMFTEncoder()
	defer enc.Close()

	w, h, fps, br := 1280, 720, 30, 2500000
	if err := enc.Init(w, h, fps, br); err != nil {
		t.Fatalf("enc.Init failed: %v", err)
	}

	type mftOutputStreamInfo struct {
		dwFlags     uint32
		cbSize      uint32
		cbAlignment uint32
	}
	var streamInfo mftOutputStreamInfo
	r := comCall(enc.mft, 7, 0, uintptr(unsafe.Pointer(&streamInfo)))
	t.Logf("GetOutputStreamInfo(0): ret=0x%08X, dwFlags=0x%X, cbSize=%d, cbAlignment=%d", r, streamInfo.dwFlags, streamInfo.cbSize, streamInfo.cbAlignment)

	// Create dummy BGRA frame
	bgra := make([]byte, w*h*4)
	for i := range bgra {
		bgra[i] = byte(i)
	}

	totalNalus := 0
	keyframeIndices := []int{}

	// Encode 10 frames
	for i := 0; i < 10; i++ {
		if i == 5 {
			enc.RequestKeyFrame()
		}
		pts := time.Duration(i) * (time.Second / time.Duration(fps))
		nalus, isKey, err := enc.Encode(bgra, pts)
		if err != nil {
			t.Fatalf("enc.Encode(frame %d) failed: %v", i, err)
		}
		if isKey {
			keyframeIndices = append(keyframeIndices, i)
		}
		totalNalus += len(nalus)
		t.Logf("Frame %d: got %d NALUs, isKey=%v", i, len(nalus), isKey)
	}

	t.Logf("Total NALUs produced: %d, keyframes at frames: %v", totalNalus, keyframeIndices)
	if totalNalus == 0 {
		t.Errorf("Expected at least some NALUs to be produced from 10 frames")
	}
	if len(keyframeIndices) < 2 {
		t.Errorf("Expected at least 2 keyframes (initial + requested), got %d: %v", len(keyframeIndices), keyframeIndices)
	}
}

