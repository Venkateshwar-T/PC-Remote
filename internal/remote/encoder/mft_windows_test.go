//go:build windows

package encoder

import (
	"testing"

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

	// Guard the specific regression: 8 is GetAttributes, not SetOutputType.
	if vtIMFTransformSetOutputType == 8 {
		t.Error("SetOutputType must not use index 8 (that is GetAttributes -> E_POINTER)")
	}
	if vtIMFTransformSetInputType != 15 || vtIMFTransformSetOutputType != 16 {
		t.Errorf("SetInputType/SetOutputType must be 15/16, got %d/%d",
			vtIMFTransformSetInputType, vtIMFTransformSetOutputType)
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
			expected: "{48eba18e-f827-4970-b450-cb99aa1522d7}",
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
			expected: "{e272446c-e767-4972-b012-18d5226f0e39}",
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
