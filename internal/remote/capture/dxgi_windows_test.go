//go:build windows

package capture

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestDXGI_VTableIndices documents and locks down the absolute COM vtable indices
// used by the capture path. Indices continue from the parent interface rather than
// restarting, so an off-by-N here calls a completely different method and fails
// with DXGI_ERROR_INVALID_CALL (0x887A0001) at runtime.
func TestDXGI_VTableIndices(t *testing.T) {
	// Method counts per interface, in inheritance order. IUnknown is the base.
	layout := map[string]struct {
		methods int
		first   int
	}{
		"IUnknown":     {3, 0},
		"IDXGIObject":  {4, 3},
		"IDXGIOutput":  {12, 7},
		"IDXGIOutput1": {4, 19},
	}

	// Verify the layout assumptions themselves.
	if got := layout["IDXGIOutput"].first + layout["IDXGIOutput"].methods - 1; got != 18 {
		t.Fatalf("IDXGIOutput should end at index 18, computed %d", got)
	}
	if got := layout["IDXGIOutput1"].first; got != 19 {
		t.Fatalf("IDXGIOutput1 should start at 19, computed %d", got)
	}

	// GetFrameStatistics is the LAST method of IDXGIOutput (index 18). The old code
	// used 18 for DuplicateOutput, which is exactly this method -> INVALID_CALL.
	const getFrameStatistics = 18
	const duplicateOutput = 22

	if vtIDXGIOutput1DuplicateOutput != duplicateOutput {
		t.Fatalf("DuplicateOutput must use vtable index %d, got %d", duplicateOutput, vtIDXGIOutput1DuplicateOutput)
	}
	if duplicateOutput == getFrameStatistics {
		t.Fatal("DuplicateOutput must not collide with GetFrameStatistics")
	}

	// DuplicateOutput is the last method of IDXGIOutput1.
	if want := layout["IDXGIOutput1"].first + layout["IDXGIOutput1"].methods - 1; want != vtIDXGIOutput1DuplicateOutput {
		t.Fatalf("DuplicateOutput should be the last IDXGIOutput1 method (index %d), got %d", want, vtIDXGIOutput1DuplicateOutput)
	}
}

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
