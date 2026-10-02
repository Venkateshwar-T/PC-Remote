package encoder

import (
	"errors"
	"time"
)

var (
	ErrEncoderUninitialized = errors.New("encoder is not initialized")
	ErrInvalidDimensions   = errors.New("invalid video dimensions (must be positive even integers)")
)

// VideoEncoder defines the interface for compressing raw frames into H.264 NALUs.
type VideoEncoder interface {
	Init(width, height, fps, bitrate int) error
	Encode(bgra []byte, pts time.Duration) ([][]byte, bool, error)
	RequestKeyFrame()
	Close() error
}

// ConvertBGRAToNV12 converts a 32-bit BGRA buffer into Y and interleaved UV planes (NV12).
func ConvertBGRAToNV12(bgra []byte, width, height, stride int, outNV12 []byte) {
	ySize := width * height
	uvStart := ySize

	for y := 0; y < height; y++ {
		rowOffset := y * stride
		yOffset := y * width
		isEvenRow := (y & 1) == 0
		uvRowOffset := uvStart + (y/2)*width

		for x := 0; x < width; x++ {
			px := rowOffset + x*4
			b := int(bgra[px])
			g := int(bgra[px+1])
			r := int(bgra[px+2])

			// Rec. 601 Color Conversion
			yVal := ((66*r + 129*g + 25*b + 128) >> 8) + 16
			if yVal < 16 {
				yVal = 16
			} else if yVal > 235 {
				yVal = 235
			}
			outNV12[yOffset+x] = byte(yVal)

			if isEvenRow && (x&1) == 0 {
				uVal := ((-38*r - 74*g + 112*b + 128) >> 8) + 128
				vVal := ((112*r - 94*g - 18*b + 128) >> 8) + 128
				if uVal < 16 {
					uVal = 16
				} else if uVal > 240 {
					uVal = 240
				}
				if vVal < 16 {
					vVal = 16
				} else if vVal > 240 {
					vVal = 240
				}

				uvIdx := uvRowOffset + x
				outNV12[uvIdx] = byte(uVal)
				outNV12[uvIdx+1] = byte(vVal)
			}
		}
	}
}

// SplitAnnexBNALUs splits an Annex-B formatted byte stream into individual NAL units.
func SplitAnnexBNALUs(data []byte) [][]byte {
	var nalus [][]byte
	n := len(data)
	if n < 4 {
		return nil
	}

	start := -1
	for i := 0; i <= n-4; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			prefixLen := 0
			if data[i+2] == 1 {
				prefixLen = 3
			} else if data[i+2] == 0 && data[i+3] == 1 {
				prefixLen = 4
			}

			if prefixLen > 0 {
				if start != -1 && i > start {
					nalus = append(nalus, data[start:i])
				}
				start = i + prefixLen
				i += prefixLen - 1
			}
		}
	}

	if start != -1 && start < n {
		nalus = append(nalus, data[start:n])
	}

	return nalus
}
