//go:build windows

package input

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procSendInput        = modUser32.NewProc("SendInput")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
)

const (
	INPUT_MOUSE = 0

	MOUSEEVENTF_MOVE        = 0x0001
	MOUSEEVENTF_LEFTDOWN    = 0x0002
	MOUSEEVENTF_LEFTUP      = 0x0004
	MOUSEEVENTF_RIGHTDOWN   = 0x0008
	MOUSEEVENTF_RIGHTUP     = 0x0010
	MOUSEEVENTF_MIDDLEDOWN  = 0x0020
	MOUSEEVENTF_MIDDLEUP    = 0x0040
	MOUSEEVENTF_WHEEL       = 0x0800
	MOUSEEVENTF_HWHEEL      = 0x1000
	MOUSEEVENTF_VIRTUALDESK = 0x4000
	MOUSEEVENTF_ABSOLUTE    = 0x8000

	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
)

type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type hardwareInput struct {
	uMsg    uint32
	wParamL uint16
	wParamH uint16
}

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type inputUnion struct {
	mi mouseInput
}

type winInput struct {
	inputType uint32
	u         inputUnion
}

// Injector handles safe Win32 mouse and keyboard injection into the active session.
type Injector struct {
	mu           sync.Mutex
	screenWidth  int32
	screenHeight int32
	virtX        int32
	virtY        int32
	virtWidth    int32
	virtHeight   int32
}

// NewInjector initializes an input injector with current display dimensions.
func NewInjector() *Injector {
	inj := &Injector{}
	inj.RefreshMetrics()
	return inj
}

// RefreshMetrics queries current Windows display and virtual screen bounds.
func (inj *Injector) RefreshMetrics() {
	inj.mu.Lock()
	defer inj.mu.Unlock()

	w, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))
	vx, _, _ := procGetSystemMetrics.Call(uintptr(SM_XVIRTUALSCREEN))
	vy, _, _ := procGetSystemMetrics.Call(uintptr(SM_YVIRTUALSCREEN))
	vw, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXVIRTUALSCREEN))
	vh, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYVIRTUALSCREEN))

	inj.screenWidth = int32(w)
	inj.screenHeight = int32(h)
	inj.virtX = int32(vx)
	inj.virtY = int32(vy)
	inj.virtWidth = int32(vw)
	inj.virtHeight = int32(vh)

	if inj.virtWidth <= 0 {
		inj.virtWidth = inj.screenWidth
	}
	if inj.virtHeight <= 0 {
		inj.virtHeight = inj.screenHeight
	}
}

// HandleInput validates and executes an InputMessage.
func (inj *Injector) HandleInput(msg *InputMessage) error {
	if msg == nil {
		return errors.New("nil input message")
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	inj.mu.Lock()
	defer inj.mu.Unlock()

	switch msg.Type {
	case "mouse_move":
		if msg.Mode == ModeRelative {
			return inj.sendMouse(int32(msg.DX), int32(msg.DY), 0, MOUSEEVENTF_MOVE)
		} else if msg.Mode == ModeAbsolute {
			// Convert normalized [0, 1] coordinates to Windows normalized [0, 65535] virtual screen space
			absX := int32((msg.X * float64(inj.screenWidth)) * 65535.0 / float64(inj.virtWidth))
			absY := int32((msg.Y * float64(inj.screenHeight)) * 65535.0 / float64(inj.virtHeight))
			return inj.sendMouse(absX, absY, 0, MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK)
		}

	case "mouse_click":
		var downFlag, upFlag uint32
		switch msg.Button {
		case ButtonLeft:
			downFlag, upFlag = MOUSEEVENTF_LEFTDOWN, MOUSEEVENTF_LEFTUP
		case ButtonRight:
			downFlag, upFlag = MOUSEEVENTF_RIGHTDOWN, MOUSEEVENTF_RIGHTUP
		case ButtonMiddle:
			downFlag, upFlag = MOUSEEVENTF_MIDDLEDOWN, MOUSEEVENTF_MIDDLEUP
		}
		if err := inj.sendMouse(0, 0, 0, downFlag); err != nil {
			return err
		}
		return inj.sendMouse(0, 0, 0, upFlag)

	case "mouse_down":
		var downFlag uint32
		switch msg.Button {
		case ButtonLeft:
			downFlag = MOUSEEVENTF_LEFTDOWN
		case ButtonRight:
			downFlag = MOUSEEVENTF_RIGHTDOWN
		case ButtonMiddle:
			downFlag = MOUSEEVENTF_MIDDLEDOWN
		}
		return inj.sendMouse(0, 0, 0, downFlag)

	case "mouse_up":
		var upFlag uint32
		switch msg.Button {
		case ButtonLeft:
			upFlag = MOUSEEVENTF_LEFTUP
		case ButtonRight:
			upFlag = MOUSEEVENTF_RIGHTUP
		case ButtonMiddle:
			upFlag = MOUSEEVENTF_MIDDLEUP
		}
		return inj.sendMouse(0, 0, 0, upFlag)

	case "scroll":
		if msg.DY != 0 {
			// Vertical scroll: Windows WHEEL_DELTA is 120
			wheelDelta := int32(msg.DY)
			if err := inj.sendMouse(0, 0, uint32(wheelDelta), MOUSEEVENTF_WHEEL); err != nil {
				return err
			}
		}
		if msg.DX != 0 {
			// Horizontal scroll
			wheelDelta := int32(msg.DX)
			if err := inj.sendMouse(0, 0, uint32(wheelDelta), MOUSEEVENTF_HWHEEL); err != nil {
				return err
			}
		}
		return nil
	}

	return nil
}

func (inj *Injector) sendMouse(dx, dy int32, data, flags uint32) error {
	var in winInput
	in.inputType = INPUT_MOUSE
	in.u.mi.dx = dx
	in.u.mi.dy = dy
	in.u.mi.mouseData = data
	in.u.mi.dwFlags = flags

	ret, _, err := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&in)),
		unsafe.Sizeof(in),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}
	return nil
}
