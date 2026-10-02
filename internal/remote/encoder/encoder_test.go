package encoder

import (
	"bytes"
	"testing"
)

func TestConvertBGRAToNV12(t *testing.T) {
	w, h := 4, 4
	bgra := make([]byte, w*h*4)
	// Fill with solid white: B=255, G=255, R=255, A=255
	for i := 0; i < len(bgra); i += 4 {
		bgra[i] = 255
		bgra[i+1] = 255
		bgra[i+2] = 255
		bgra[i+3] = 255
	}

	nv12 := make([]byte, (w*h*3)/2)
	ConvertBGRAToNV12(bgra, w, h, w*4, nv12)

	// In Rec. 601, white Y should be clamped to 235
	for i := 0; i < w*h; i++ {
		if nv12[i] != 235 {
			t.Fatalf("expected white Y to be 235, got %d at pixel %d", nv12[i], i)
		}
	}

	// For white, U and V should be neutral ~128
	for i := w * h; i < len(nv12); i++ {
		if nv12[i] < 126 || nv12[i] > 130 {
			t.Fatalf("expected neutral chroma ~128, got %d at offset %d", nv12[i], i)
		}
	}
}

func TestSplitAnnexBNALUs(t *testing.T) {
	// Sample Annex-B stream:
	// NAL 1: 00 00 00 01 07 01 02 (SPS, 3 bytes)
	// NAL 2: 00 00 00 01 08 03 04 (PPS, 3 bytes)
	// NAL 3: 00 00 01 05 06 07 (IDR slice, 3-byte prefix, 3 bytes)
	raw := []byte{
		0x00, 0x00, 0x00, 0x01, 0x07, 0x01, 0x02,
		0x00, 0x00, 0x00, 0x01, 0x08, 0x03, 0x04,
		0x00, 0x00, 0x01, 0x05, 0x06, 0x07,
	}

	nalus := SplitAnnexBNALUs(raw)
	if len(nalus) != 3 {
		t.Fatalf("expected 3 NAL units, got %d", len(nalus))
	}

	expected1 := []byte{0x07, 0x01, 0x02}
	if !bytes.Equal(nalus[0], expected1) {
		t.Fatalf("NAL 1 mismatch: got %v, expected %v", nalus[0], expected1)
	}

	expected2 := []byte{0x08, 0x03, 0x04}
	if !bytes.Equal(nalus[1], expected2) {
		t.Fatalf("NAL 2 mismatch: got %v, expected %v", nalus[1], expected2)
	}

	expected3 := []byte{0x05, 0x06, 0x07}
	if !bytes.Equal(nalus[2], expected3) {
		t.Fatalf("NAL 3 mismatch: got %v, expected %v", nalus[2], expected3)
	}
}
