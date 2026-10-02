//go:build windows

package encoder

import (
	"testing"

	"golang.org/x/sys/windows"
)

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
			expected: "{bf803747-0e08-4639-a819-399120e6c436}",
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
