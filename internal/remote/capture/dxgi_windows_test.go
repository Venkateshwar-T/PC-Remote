//go:build windows

package capture

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestDXGI_GUIDsAgainstWindowsSDK(t *testing.T) {
	tests := []struct {
		name     string
		actual   windows.GUID
		expected string
	}{
		{
			name:     "IID_IDXGIDevice",
			actual:   iidIDXGIDevice,
			expected: "{54ec77fa-1377-44e6-8c32-88fd5f44c84c}",
		},
		{
			name:     "IID_IDXGIOutput1",
			actual:   iidIDXGIOutput1,
			expected: "{00cddea8-939b-4b83-a340-a685226666cc}",
		},
		{
			name:     "IID_ID3D11Texture2D",
			actual:   iidID3D11Texture2D,
			expected: "{6f15aff2-d20e-42fa-9e2e-5730d0800a23}",
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
