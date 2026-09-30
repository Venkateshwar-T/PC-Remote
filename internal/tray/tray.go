package tray

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"syscall"
	"unsafe"

	qrcode "github.com/skip2/go-qrcode"
	"laptopcontrol/internal/config"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modDwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procDwmSetWindowAttribute = modDwmapi.NewProc("DwmSetWindowAttribute")
	procGetModuleHandleW      = modKernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW      = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW       = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW        = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow         = modUser32.NewProc("DestroyWindow")
	procShowWindow            = modUser32.NewProc("ShowWindow")
	procUpdateWindow          = modUser32.NewProc("UpdateWindow")
	procGetMessageW           = modUser32.NewProc("GetMessageW")
	procTranslateMessage      = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW      = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage       = modUser32.NewProc("PostQuitMessage")
	procPostMessageW          = modUser32.NewProc("PostMessageW")
	procSendMessageW          = modUser32.NewProc("SendMessageW")
	procMessageBoxW           = modUser32.NewProc("MessageBoxW")
	procLoadCursorW           = modUser32.NewProc("LoadCursorW")
	procLoadIconW             = modUser32.NewProc("LoadIconW")
	procLoadImageW            = modUser32.NewProc("LoadImageW")
	procCreatePopupMenu       = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW           = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu        = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu           = modUser32.NewProc("DestroyMenu")
	procGetCursorPos          = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procAllowSetForegroundWindow = modUser32.NewProc("AllowSetForegroundWindow")
	procGetSystemMetrics      = modUser32.NewProc("GetSystemMetrics")
	procGetClientRect         = modUser32.NewProc("GetClientRect")
	procAdjustWindowRectEx    = modUser32.NewProc("AdjustWindowRectEx")
	procBeginPaint            = modUser32.NewProc("BeginPaint")
	procEndPaint              = modUser32.NewProc("EndPaint")
	procFillRect              = modUser32.NewProc("FillRect")
	procDrawTextW             = modUser32.NewProc("DrawTextW")
	procSetTextColor          = modGdi32.NewProc("SetTextColor")
	procSetBkMode             = modGdi32.NewProc("SetBkMode")
	procCreateSolidBrush      = modGdi32.NewProc("CreateSolidBrush")
	procSetDIBitsToDevice     = modGdi32.NewProc("SetDIBitsToDevice")
	procDeleteObject          = modGdi32.NewProc("DeleteObject")
	procShell_NotifyIconW     = modShell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW        = modShell32.NewProc("ExtractIconExW")
	procCreateFontW           = modGdi32.NewProc("CreateFontW")
	procSelectObject          = modGdi32.NewProc("SelectObject")
	procGetForegroundWindow   = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput     = modUser32.NewProc("AttachThreadInput")
	procGetCurrentThreadId    = modKernel32.NewProc("GetCurrentThreadId")
	procBringWindowToTop      = modUser32.NewProc("BringWindowToTop")
	procSetWindowPos          = modUser32.NewProc("SetWindowPos")
	procSwitchToThisWindow    = modUser32.NewProc("SwitchToThisWindow")
	procSetFocus              = modUser32.NewProc("SetFocus")
)

const (
	IMAGE_ICON          = 1
	LR_DEFAULTCOLOR     = 0x0000
	WM_SETICON          = 0x0080
	ICON_SMALL          = 0
	ICON_BIG            = 1
	WM_APP              = 0x8000
	WM_TRAYICON         = WM_APP + 1
	WM_USER_SHOW_PAIR   = WM_APP + 2
	NIM_ADD             = 0x00000000
	NIM_MODIFY          = 0x00000001
	NIM_DELETE          = 0x00000002
	NIF_MESSAGE         = 0x00000001
	NIF_ICON            = 0x00000002
	NIF_TIP             = 0x00000004
	NIF_INFO            = 0x00000010
	NIIF_NONE           = 0x00000000
	NIIF_INFO           = 0x00000001
	NIIF_WARNING        = 0x00000002
	NIIF_ERROR          = 0x00000003
	NIIF_USER           = 0x00000004
	NIIF_LARGE_ICON     = 0x00000020
	ASFW_ANY            = 0xFFFFFFFF
	HWND_TOPMOST        = ^uintptr(0)
	HWND_NOTOPMOST      = ^uintptr(1)
	SWP_NOMOVE          = 0x0002
	SWP_NOSIZE          = 0x0001
	SWP_SHOWWINDOW      = 0x0040
	WM_LBUTTONUP        = 0x0202
	WM_RBUTTONUP        = 0x0205
	WM_LBUTTONDBLCLK    = 0x0203
	WM_PAINT            = 0x000F
	WM_CLOSE            = 0x0010
	WM_COMMAND          = 0x0111
	WM_DESTROY          = 0x0002
	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_MINIMIZEBOX      = 0x00020000
	WS_POPUP            = 0x80000000
	WS_VISIBLE          = 0x10000000
	SW_HIDE             = 0
	SW_SHOW             = 5
	SW_RESTORE          = 9
	MF_STRING           = 0x00000000
	MF_SEPARATOR        = 0x00000800
	TPM_RIGHTBUTTON     = 0x0002
	TPM_BOTTOMALIGN     = 0x0020
	DT_CENTER           = 0x00000001
	DT_SINGLELINE       = 0x00000020
	DT_WORDBREAK        = 0x00000010
	ID_TRAY_SHOW_PAIR   = 2001
	ID_TRAY_CHANGE_PIN  = 2004
	ID_TRAY_OPEN_DASH   = 2002
	ID_TRAY_EXIT        = 2003
)

type POINT struct {
	X int32
	Y int32
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type NOTIFYICONDATAW struct {
	Size             uint32
	Wnd              uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GuidItem         [16]byte
	BalloonIcon      uintptr
}

type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

type Manager struct {
	cfg         *config.Config
	pairingUrl  string
	silent      bool
	onExit      func()
	hwnd        uintptr
	nid         NOTIFYICONDATAW
	hIconBig    uintptr
	hIconSm     uintptr
	qrPixels    []byte
	qrWidth     int
	qrHeight    int
	isShown     bool
}

var globalManager *Manager

func stringToUTF16Ptr(s string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(s)
	return ptr
}

func copyUTF16String(dest []uint16, src string) {
	chars, _ := syscall.UTF16FromString(src)
	copy(dest, chars)
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if globalManager == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			globalManager.ShowPairingWindow()
		case WM_RBUTTONUP:
			globalManager.showContextMenu()
		}
		return 0

	case WM_COMMAND:
		cmdId := uint32(wParam & 0xFFFF)
		switch cmdId {
		case ID_TRAY_SHOW_PAIR:
			globalManager.ShowPairingWindow()
		case ID_TRAY_CHANGE_PIN:
			go globalManager.PromptChangePin()
		case ID_TRAY_OPEN_DASH:
			exec.Command("rundll32", "url.dll,FileProtocolHandler", globalManager.pairingUrl).Start()
		case ID_TRAY_EXIT:
			globalManager.Close()
		}
		return 0

	case WM_CLOSE:
		// Don't kill process on close; just hide to tray!
		procShowWindow.Call(hwnd, SW_HIDE)
		globalManager.isShown = false
		return 0

	case WM_PAINT:
		globalManager.onPaint(hwnd)
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func NewManager(cfg *config.Config, pairingUrl string, silent bool, onExit func()) *Manager {
	m := &Manager{
		cfg:        cfg,
		pairingUrl: pairingUrl,
		silent:     silent,
		onExit:     onExit,
	}
	globalManager = m

	// Generate QR Bitmap in memory
	m.prepareQrBitmap()

	return m
}

func (m *Manager) prepareQrBitmap() {
	pngBytes, err := qrcode.Encode(m.pairingUrl, qrcode.Medium, 260)
	if err != nil {
		return
	}

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	m.qrWidth = w
	m.qrHeight = h

	// GDI expects 32-bit BGRA top-down
	pixels := make([]byte, w*h*4)
	idx := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			pixels[idx+0] = byte(b >> 8)
			pixels[idx+1] = byte(g >> 8)
			pixels[idx+2] = byte(r >> 8)
			pixels[idx+3] = byte(a >> 8)
			idx += 4
		}
	}
	m.qrPixels = pixels

	// Release intermediate image decoder buffers immediately back to OS
	debug.FreeOSMemory()
}

func (m *Manager) showContextMenu() {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_SHOW_PAIR, uintptr(unsafe.Pointer(stringToUTF16Ptr("Show Pairing QR Code"))))
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_CHANGE_PIN, uintptr(unsafe.Pointer(stringToUTF16Ptr("Change Master PIN..."))))
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_OPEN_DASH, uintptr(unsafe.Pointer(stringToUTF16Ptr("Open Web Dashboard"))))
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_EXIT, uintptr(unsafe.Pointer(stringToUTF16Ptr("Exit PC Remote"))))

	procSetForegroundWindow.Call(m.hwnd)
	procTrackPopupMenu.Call(hMenu, TPM_RIGHTBUTTON|TPM_BOTTOMALIGN, uintptr(pt.X), uintptr(pt.Y), 0, m.hwnd, 0)
}

func forceForeground(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	procAllowSetForegroundWindow.Call(ASFW_ANY)

	procShowWindow.Call(hwnd, SW_SHOW)
	procShowWindow.Call(hwnd, SW_RESTORE)

	// Step 1: Momentarily elevate to topmost then drop topmost so it sits at the peak of the Z-order
	procSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
	procSetWindowPos.Call(hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)

	// Step 2: Attach to foreground thread input queue if needed to bypass foreground lockout
	fgWnd, _, _ := procGetForegroundWindow.Call()
	curThread, _, _ := procGetCurrentThreadId.Call()
	var fgThread uintptr
	if fgWnd != 0 {
		fgThread, _, _ = procGetWindowThreadProcessId.Call(fgWnd, 0)
	}

	if fgThread != 0 && fgThread != curThread {
		procAttachThreadInput.Call(curThread, fgThread, 1)
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procSetFocus.Call(hwnd)
		procSwitchToThisWindow.Call(hwnd, 1)
		procAttachThreadInput.Call(curThread, fgThread, 0)
	} else {
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procSetFocus.Call(hwnd)
		procSwitchToThisWindow.Call(hwnd, 1)
	}
	procUpdateWindow.Call(hwnd)
}

func (m *Manager) ShowPairingWindow() {
	if m.hwnd == 0 {
		return
	}
	forceForeground(m.hwnd)
	m.isShown = true
}

func (m *Manager) onPaint(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rcClient RECT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rcClient)))
	clientWidth := int(rcClient.Right - rcClient.Left)
	if clientWidth <= 0 {
		clientWidth = 460
	}

	// Fill background with matte dark #09090b
	bgBrush, _, _ := procCreateSolidBrush.Call(0x000b0909) // RGB(9, 9, 11) in BGR
	if bgBrush != 0 {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rcClient)), bgBrush)
		procDeleteObject.Call(bgBrush)
	}

	minusOne := uintptr(0xFFFFFFFF)
	procSetBkMode.Call(hdc, 1) // TRANSPARENT

	// 1. Draw Title (Prominent 26px Segoe UI, Semi-Bold) - mathematically centered across entire client width
	procSetTextColor.Call(hdc, 0x00f5f4f4) // RGB(244, 244, 245)
	hFontTitle, _, _ := procCreateFontW.Call(28, 0, 0, 0, 600, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(stringToUTF16Ptr("Segoe UI"))))
	if hFontTitle != 0 {
		oldFont, _, _ := procSelectObject.Call(hdc, hFontTitle)
		var rTitle RECT
		rTitle.Left = 0
		rTitle.Top = 24
		rTitle.Right = int32(clientWidth)
		rTitle.Bottom = 58
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(stringToUTF16Ptr("Pair Your Phone"))), minusOne, uintptr(unsafe.Pointer(&rTitle)), DT_CENTER|DT_SINGLELINE)
		procSelectObject.Call(hdc, oldFont)
		procDeleteObject.Call(hFontTitle)
	}

	// 2. Draw Subtitle (Clear 16px Segoe UI, #a1a1aa) - symmetrical margins
	procSetTextColor.Call(hdc, 0x00aaa1a1) // RGB(161, 161, 170)
	hFontSub, _, _ := procCreateFontW.Call(20, 0, 0, 0, 400, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(stringToUTF16Ptr("Segoe UI"))))
	if hFontSub != 0 {
		oldFont, _, _ := procSelectObject.Call(hdc, hFontSub)
		var rSub RECT
		rSub.Left = 16
		rSub.Top = 60
		rSub.Right = int32(clientWidth) - 16
		rSub.Bottom = 88
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(stringToUTF16Ptr("Scan with your phone camera on same Wi-Fi"))), minusOne, uintptr(unsafe.Pointer(&rSub)), DT_CENTER|DT_SINGLELINE)
		procSelectObject.Call(hdc, oldFont)
		procDeleteObject.Call(hFontSub)
	}

	// 3. Draw QR Code using SetDIBitsToDevice (260x260) - mathematically centered
	if len(m.qrPixels) > 0 {
		var bmi BITMAPINFO
		bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
		bmi.Header.Width = int32(m.qrWidth)
		bmi.Header.Height = -int32(m.qrHeight) // Top-down
		bmi.Header.Planes = 1
		bmi.Header.BitCount = 32
		bmi.Header.Compression = 0 // BI_RGB

		qrX := (clientWidth - m.qrWidth) / 2
		qrY := 98

		procSetDIBitsToDevice.Call(
			hdc,
			uintptr(qrX), uintptr(qrY),
			uintptr(m.qrWidth), uintptr(m.qrHeight),
			0, 0, 0, uintptr(m.qrHeight),
			uintptr(unsafe.Pointer(&m.qrPixels[0])),
			uintptr(unsafe.Pointer(&bmi)),
			0,
		)
	}

	// 4. Draw Device Name & Status (Bold 18px, Emerald Green #22c55e) - mathematically centered
	procSetTextColor.Call(hdc, 0x005ec522) // Emerald Green #22c55e
	hFontDevice, _, _ := procCreateFontW.Call(20, 0, 0, 0, 600, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(stringToUTF16Ptr("Segoe UI"))))
	if hFontDevice != 0 {
		oldFont, _, _ := procSelectObject.Call(hdc, hFontDevice)
		var rDevice RECT
		rDevice.Left = 0
		rDevice.Top = 380
		rDevice.Right = int32(clientWidth)
		rDevice.Bottom = 408
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(stringToUTF16Ptr(fmt.Sprintf("• %s", m.cfg.DeviceName)))), minusOne, uintptr(unsafe.Pointer(&rDevice)), DT_CENTER|DT_SINGLELINE)
		procSelectObject.Call(hdc, oldFont)
		procDeleteObject.Call(hFontDevice)
	}

	// 5. Draw Instructions / Hint (Legible 15px Segoe UI, #a1a1aa) - symmetrical margins
	procSetTextColor.Call(hdc, 0x00aaa1a1) // RGB(161, 161, 170)
	hFontHint, _, _ := procCreateFontW.Call(18, 0, 0, 0, 400, 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(stringToUTF16Ptr("Segoe UI"))))
	if hFontHint != 0 {
		oldFont, _, _ := procSelectObject.Call(hdc, hFontHint)
		var rHint RECT
		rHint.Left = 24
		rHint.Top = 418
		rHint.Right = int32(clientWidth) - 24
		rHint.Bottom = 490
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(stringToUTF16Ptr("Enter your 6-digit Master PIN on phone.\nClosing this window keeps PC Remote active in the tray."))), minusOne, uintptr(unsafe.Pointer(&rHint)), DT_CENTER|DT_WORDBREAK)
		procSelectObject.Call(hdc, oldFont)
		procDeleteObject.Call(hFontHint)
	}
}

func (m *Manager) Run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := "PCRemoteWindowClass"
	cursor, _, _ := procLoadCursorW.Call(0, 32512) // IDC_ARROW

	// Load custom embedded high-DPI modern laptop icon from PE resources or executable
	var hIconBig, hIconSm uintptr
	exePath, err := os.Executable()
	if err == nil {
		exePathPtr, _ := syscall.UTF16PtrFromString(exePath)
		procExtractIconExW.Call(
			uintptr(unsafe.Pointer(exePathPtr)),
			0,
			uintptr(unsafe.Pointer(&hIconBig)),
			uintptr(unsafe.Pointer(&hIconSm)),
			1,
		)
	}

	if hIconBig == 0 {
		hIconBig, _, _ = procLoadIconW.Call(0, 32512) // IDI_APPLICATION fallback
	}
	if hIconSm == 0 {
		hIconSm = hIconBig
	}
	m.hIconBig = hIconBig
	m.hIconSm = hIconSm

	var wc WNDCLASSEXW
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.WndProc = syscall.NewCallback(wndProc)
	wc.Instance = hInst
	wc.Cursor = cursor
	wc.Icon = hIconBig
	wc.IconSm = hIconSm
	wc.ClassName = stringToUTF16Ptr(className)

	atom, _, errReg := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		errno, ok := errReg.(syscall.Errno)
		if !ok || errno != 1410 { // 1410: ERROR_CLASS_ALREADY_EXISTS
			return
		}
	}

	// Calculate window dimensions using AdjustWindowRectEx so the client area is exactly 460 x 580
	style := uint32(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX)
	var wr RECT
	wr.Right = 460
	wr.Bottom = 580
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&wr)), uintptr(style), 0, 0)
	winWidth := uintptr(wr.Right - wr.Left)
	winHeight := uintptr(wr.Bottom - wr.Top)

	// Calculate center screen coordinates
	screenWidth, _, _ := procGetSystemMetrics.Call(0)  // SM_CXSCREEN
	screenHeight, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN

	posX := uintptr(100)
	posY := uintptr(100)
	if screenWidth > winWidth {
		posX = (screenWidth - winWidth) / 2
	}
	if screenHeight > winHeight {
		posY = (screenHeight - winHeight) / 2
	}

	// Create native centered window
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(stringToUTF16Ptr(className))),
		uintptr(unsafe.Pointer(stringToUTF16Ptr("PC Remote"))),
		uintptr(style),
		posX, posY,
		winWidth, winHeight,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		return
	}
	m.hwnd = hwnd

	// Enable modern immersive dark title bar on Windows 10 (20H1+) and Windows 11
	var darkMode int32 = 1
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&darkMode)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&darkMode)), 4)

	// Explicitly assign modern title bar icon & taskbar icon
	procSendMessageW.Call(hwnd, WM_SETICON, ICON_BIG, hIconBig)
	procSendMessageW.Call(hwnd, WM_SETICON, ICON_SMALL, hIconSm)

	// Add Tray Icon
	m.nid.Size = uint32(unsafe.Sizeof(m.nid))
	m.nid.Wnd = hwnd
	m.nid.ID = 1
	m.nid.Flags = NIF_MESSAGE | NIF_ICON | NIF_TIP | NIF_INFO
	m.nid.CallbackMessage = WM_TRAYICON
	m.nid.Icon = hIconSm
	m.nid.BalloonIcon = hIconBig
	m.nid.InfoFlags = NIIF_USER | NIIF_LARGE_ICON
	copyUTF16String(m.nid.Tip[:], "PC Remote (Running)")
	copyUTF16String(m.nid.InfoTitle[:], "PC Remote Active")
	copyUTF16String(m.nid.Info[:], "Click the tray icon anytime to pair your phone.")

	procShell_NotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&m.nid)))

	// Show pairing window on launch unless silent mode is requested
	if !m.silent {
		m.ShowPairingWindow()
	}

	// Win32 Message Loop
	var msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      POINT
	}

	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Remove tray icon on exit
	procShell_NotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&m.nid)))
	if m.onExit != nil {
		m.onExit()
	}
}

func (m *Manager) ShowNotification(title, msg string) {
	if m.hwnd == 0 {
		return
	}
	m.nid.Flags = NIF_INFO
	m.nid.InfoFlags = NIIF_USER | NIIF_LARGE_ICON
	m.nid.BalloonIcon = m.hIconBig
	copyUTF16String(m.nid.InfoTitle[:], title)
	copyUTF16String(m.nid.Info[:], msg)
	procShell_NotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&m.nid)))
}

func (m *Manager) Close() {
	if m.hwnd != 0 {
		procPostMessageW.Call(m.hwnd, WM_DESTROY, 0, 0)
	}
}

func (m *Manager) PromptChangePin() {
	title := "PC Remote - Change Master PIN"
	header := "Change Master PIN"
	desc := fmt.Sprintf("Enter a new 6-digit Master PIN for %s.\nYour phone remote will need this new PIN on its next connection.", m.cfg.DeviceName)
	saveBtn := "Confirm & Update"
	cancelBtn := "Cancel"

	newPin, err := showPinForm(title, header, desc, saveBtn, cancelBtn)
	if err != nil || len(newPin) != 6 || !isAllDigits(newPin) {
		return // User cancelled
	}

	if err := m.cfg.SetPin(newPin); err != nil {
		procMessageBoxW.Call(
			m.hwnd,
			uintptr(unsafe.Pointer(stringToUTF16Ptr("Failed to update PIN: "+err.Error()))),
			uintptr(unsafe.Pointer(stringToUTF16Ptr("PC Remote - Error"))),
			0x00000010|0x00040000|0x00010000) // MB_ICONERROR | MB_TOPMOST | MB_SETFOREGROUND
		return
	}

	m.ShowNotification("Master PIN Updated", fmt.Sprintf("New PIN: %s\nUse this new PIN when connecting from your phone remote.", newPin))
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
