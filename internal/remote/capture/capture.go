package capture

import (
	"errors"
	"time"
)

var (
	ErrTimeout    = errors.New("timeout waiting for new frame")
	ErrAccessLost = errors.New("DXGI desktop duplication access lost")
)

// CapturedFrame contains the raw captured screen buffer and pointer metadata.
type CapturedFrame struct {
	Width       int
	Height      int
	Data        []byte // BGRA pixel buffer
	Stride      int
	CursorX     int32
	CursorY     int32
	CursorVis   bool
	Timestamp   time.Time
}

// ScreenCapture defines the display capture interface.
type ScreenCapture interface {
	Init() error
	AcquireFrame(timeoutMs uint32) (*CapturedFrame, error)
	ReleaseFrame()
	Dimensions() (int, int)
	Close() error
}
