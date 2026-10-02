//go:build !windows

package encoder

import (
	"errors"
	"time"
)

type MFTEncoder struct{}

func NewMFTEncoder() *MFTEncoder {
	return &MFTEncoder{}
}

func (e *MFTEncoder) Init(width, height, fps, bitrate int) error {
	return errors.New("MFT encoder is only supported on Windows")
}

func (e *MFTEncoder) Encode(bgra []byte, pts time.Duration) ([][]byte, bool, error) {
	return nil, false, errors.New("MFT encoder is only supported on Windows")
}

func (e *MFTEncoder) RequestKeyFrame() {}

func (e *MFTEncoder) Close() error {
	return nil
}
