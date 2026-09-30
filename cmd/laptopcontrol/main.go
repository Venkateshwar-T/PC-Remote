package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"laptopcontrol/internal/config"
	"laptopcontrol/internal/qr"
	"laptopcontrol/internal/relay"
	"laptopcontrol/internal/server"
	"laptopcontrol/internal/tray"
	"laptopcontrol/web"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")

	procCreateMutexW      = modKernel32.NewProc("CreateMutexW")
	procCloseHandle       = modKernel32.NewProc("CloseHandle")
	procFindWindowW       = modUser32.NewProc("FindWindowW")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procShowWindow        = modUser32.NewProc("ShowWindow")
	procPostMessageW      = modUser32.NewProc("PostMessageW")
	procSetAppUserModelID = modShell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procSetWindowPos      = modUser32.NewProc("SetWindowPos")
	procSwitchToThisWindow = modUser32.NewProc("SwitchToThisWindow")
	procAllowSetForegroundWindow = modUser32.NewProc("AllowSetForegroundWindow")
	procGetCurrentProcess        = modKernel32.NewProc("GetCurrentProcess")
	procSetProcessWorkingSetSize = modKernel32.NewProc("SetProcessWorkingSetSize")
)

func trimWorkingSet() {
	debug.FreeOSMemory()
	hProc, _, _ := procGetCurrentProcess.Call()
	if hProc != 0 {
		minusOne := ^uintptr(0)
		procSetProcessWorkingSetSize.Call(hProc, minusOne, minusOne)
	}
}

func main() {
	// Memory optimization: instruct Go runtime to actively return idle heap pages to Windows
	debug.SetGCPercent(20)

	// Set explicit application user model ID for Windows taskbar branding
	appId, _ := syscall.UTF16PtrFromString("PCRemote.App")
	procSetAppUserModelID.Call(uintptr(unsafe.Pointer(appId)))

	flagPort := flag.Int("port", 8765, "Local HTTP server port")
	flagPin := flag.String("set-pin", "", "Set a new 6-digit master PIN")
	flagReset := flag.Bool("reset", false, "Reset configuration and launch first-time setup wizard")
	flagSilent := flag.Bool("silent", false, "Start minimized in system tray without opening QR window")
	flag.Parse()

	// Determine base directory
	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	baseDir := filepath.Dir(exePath)

	// If reset requested, signal any existing instance to terminate first
	if *flagReset {
		className, _ := syscall.UTF16PtrFromString("PCRemoteWindowClass")
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
		if hwnd != 0 {
			const WM_DESTROY = 0x0002
			procPostMessageW.Call(hwnd, WM_DESTROY, 0, 0)
			time.Sleep(300 * time.Millisecond)
		}
		_ = os.Remove(filepath.Join(baseDir, "config.json"))
	}

	// Single Instance Lock: Ensure only one instance runs at any time
	mutexName, _ := syscall.UTF16PtrFromString("Local\\PCRemote_SingleInstance_Mutex")
	hMutex, _, errMutex := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	const ERROR_ALREADY_EXISTS = 183
	if errno, ok := errMutex.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		// An instance is already running: bring its pairing window to the foreground
		className, _ := syscall.UTF16PtrFromString("PCRemoteWindowClass")
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
		if hwnd != 0 {
			const (
				SW_RESTORE     = 9
				HWND_TOPMOST   = ^uintptr(0)
				HWND_NOTOPMOST = ^uintptr(1)
				SWP_NOMOVE     = 0x0002
				SWP_NOSIZE     = 0x0001
				SWP_SHOWWINDOW = 0x0040
				ASFW_ANY       = 0xFFFFFFFF
			)
			procAllowSetForegroundWindow.Call(ASFW_ANY)
			procShowWindow.Call(hwnd, SW_RESTORE)
			procSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetWindowPos.Call(hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			procSetForegroundWindow.Call(hwnd)
			procSwitchToThisWindow.Call(hwnd, 1)
		}
		if hMutex != 0 {
			procCloseHandle.Call(hMutex)
		}
		return
	}
	if hMutex != 0 {
		defer procCloseHandle.Call(hMutex)
	}

	// Load configuration
	cfg, err := config.LoadOrCreate(baseDir)
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}

	if *flagPin != "" {
		pin := *flagPin
		if len(pin) != 6 || !isNumeric(pin) {
			log.Fatalf("Invalid PIN: Master PIN must be exactly 6 numeric digits (0-9)")
		}
		if err := cfg.SetPin(pin); err != nil {
			log.Fatalf("Failed to update PIN: %v", err)
		}
		fmt.Printf("Master PIN successfully updated to: %s\n", pin)
		return
	}

	// First-Time Setup: If no Master PIN has been established, prompt user on desktop
	if !cfg.HasPin() {
		pin, ok := tray.PromptInitialPinSetup(cfg.DeviceName)
		if !ok || len(pin) != 6 || !isNumeric(pin) {
			log.Println("[Setup] Initial PIN setup was cancelled. Exiting.")
			return
		}
		if err := cfg.SetPin(pin); err != nil {
			log.Fatalf("Failed to save initial Master PIN: %v", err)
		}
	}

	// Initialize local HTTP server
	localSrv := server.NewServer(cfg, web.Assets, *flagPort)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", *flagPort),
		Handler: localSrv,
	}

	// Start local HTTP server
	go func() {
		log.Printf("[Server] Local PWA server listening on http://0.0.0.0:%d", *flagPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Server] Error: %v", err)
		}
	}()

	// Initialize and start decentralized relay client
	relayClient := relay.NewClient(cfg, nil)
	relayClient.Start()

	// Display pairing information
	pairingUrl := localSrv.GetPairingURL()
	fmt.Println("\n========================================================")
	fmt.Printf("   PC Remote v2.0 (Decentralized Native Edition)\n")
	fmt.Printf("   Device: %s\n", cfg.DeviceName)
	fmt.Printf("   Local Pairing URL: %s\n", pairingUrl)
	fmt.Println("========================================================")
	fmt.Println("\nScan with your phone on the same Wi-Fi to pair:\n")

	ansiQR := qr.GenerateTerminalANSI(pairingUrl)
	if ansiQR != "" {
		fmt.Println(ansiQR)
	}

	// Initialize Native System Tray and Dark Pairing Window
	trayMgr := tray.NewManager(cfg, pairingUrl, *flagSilent, func() {
		log.Println("[Shutdown] Cleaning up services...")
		relayClient.Stop()
		httpServer.Close()
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	})

	// Periodic memory scavenger: flushes unused working set pages back to Windows OS
	go func() {
		// Initial trim after initialization settles
		time.Sleep(3 * time.Second)
		trimWorkingSet()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			trimWorkingSet()
		}
	}()

	// Handle graceful shutdown via Ctrl+C / SIGINT
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		fmt.Println("\nShutting down PC Remote cleanly...")
		trayMgr.Close()
	}()

	// Start native message loop (0% CPU, handles tray icon and pairing window)
	trayMgr.Run()
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
