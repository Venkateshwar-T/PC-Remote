//go:build !windows

package capture

import "errors"

type DXGICapture struct{}

func NewDXGICapture() *DXGICapture {
	return &DXGICapture{}
}

func (c *DXGICapture) Init() error {
	return errors.New("DXGI capture is only supported on Windows")
}

func (c *DXGICapture) AcquireFrame(timeoutMs uint32) (*CapturedFrame, error) {
	return nil, errors.New("DXGI capture is only supported on Windows")
}

func (c *DXGICapture) ReleaseFrame() {}

func (c *DXGICapture) Dimensions() (int, int) {
	return 1920, 1080
}

func (c *DXGICapture) Close() error {
	return nil
}
