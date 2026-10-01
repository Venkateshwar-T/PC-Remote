package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/nbd-wtf/go-nostr"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/ipc"
	"laptopcontrol/internal/service"
	"laptopcontrol/internal/tray"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")

	procCreateMutexW             = modKernel32.NewProc("CreateMutexW")
	procCloseHandle              = modKernel32.NewProc("CloseHandle")
	procFindWindowW              = modUser32.NewProc("FindWindowW")
	procSetForegroundWindow      = modUser32.NewProc("SetForegroundWindow")
	procShowWindow               = modUser32.NewProc("ShowWindow")
	procPostMessageW             = modUser32.NewProc("PostMessageW")
	procSetAppUserModelID        = modShell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procSetWindowPos             = modUser32.NewProc("SetWindowPos")
	procSwitchToThisWindow       = modUser32.NewProc("SwitchToThisWindow")
	procAllowSetForegroundWindow = modUser32.NewProc("AllowSetForegroundWindow")
	procAttachConsole            = modKernel32.NewProc("AttachConsole")
)

const ATTACH_PARENT_PROCESS = ^uintptr(0)

func main() {
	// If launched from a console/terminal, attach to parent console so CLI output is visible
	ret, _, _ := procAttachConsole.Call(ATTACH_PARENT_PROCESS)
	if ret != 0 {
		os.Stdout = os.NewFile(uintptr(syscall.Stdout), "/dev/stdout")
		os.Stderr = os.NewFile(uintptr(syscall.Stderr), "/dev/stderr")
		log.SetOutput(os.Stderr)
	}

	// Parse CLI arguments
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "service", "-service", "--service":
			runServiceCommand(os.Args[2:])
			return
		case "tray", "-tray", "--tray":
			runTrayCommand(os.Args[2:])
			return
		case "install":
			exePath, err := os.Executable()
			if err != nil {
				log.Fatalf("Failed to resolve executable path: %v", err)
			}
			if err := service.InstallService(exePath); err != nil {
				log.Fatalf("Failed to install Windows service: %v", err)
			}
			fmt.Println("PC Remote background service installed successfully.")
			return
		case "uninstall":
			if err := service.UninstallService(); err != nil {
				log.Fatalf("Failed to uninstall Windows service: %v", err)
			}
			fmt.Println("PC Remote background service uninstalled successfully.")
			return
		case "start":
			if err := service.StartService(); err != nil {
				log.Fatalf("Failed to start Windows service: %v", err)
			}
			fmt.Println("PC Remote background service started.")
			return
		case "stop":
			if err := service.StopService(); err != nil {
				log.Fatalf("Failed to stop Windows service: %v", err)
			}
			fmt.Println("PC Remote background service stopped.")
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// If invoked without subcommands, detect environment:
	// If running under Windows Service Control Manager -> run as service
	// If running in interactive desktop session -> default to interactive tray
	isSvc, _ := service.IsServiceSession()
	if isSvc {
		runServiceCommand(nil)
		return
	}

	runTrayCommand(os.Args[1:])
}

func printUsage() {
	fmt.Println("PC Remote - Remote Control Daemon & Desktop Tray")
	fmt.Println("\nUsage:")
	fmt.Println("  PC-Remote.exe [command] [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  service          Run as Windows background service (SCM entry point)")
	fmt.Println("    -port=8765     Local HTTP server port (default: 8765)")
	fmt.Println("    -debug         Run service in foreground console for debugging")
	fmt.Println("  tray             Run interactive desktop system tray & pairing UI")
	fmt.Println("    -silent        Start minimized in system tray without opening QR window")
	fmt.Println("  install          Register service with Windows Service Control Manager")
	fmt.Println("  uninstall        Remove service from Windows Service Control Manager")
	fmt.Println("  start            Start installed Windows background service")
	fmt.Println("  stop             Stop running Windows background service")
}

func runServiceCommand(args []string) {
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	port := fs.Int("port", 8765, "Local HTTP server port")
	debug := fs.Bool("debug", false, "Run service in foreground console")
	_ = fs.Parse(args)

	isSvc, _ := service.IsServiceSession()
	if *debug || !isSvc {
		log.Printf("[Service] Running PC Remote daemon in debug console mode on port %d...", *port)
		daemon := service.NewDaemon(*port, ipc.PipeName, config.GetServiceConfigDir())
		if err := daemon.Start(); err != nil {
			log.Fatalf("Failed to start daemon: %v", err)
		}

		// Handle Ctrl+C for clean exit in debug console
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("[Service] Interrupted, shutting down...")
		daemon.Stop()
		return
	}

	// Normal SCM Service Execution
	ws := service.NewWindowsService(*port, ipc.PipeName, config.GetServiceConfigDir())
	if err := service.RunAsService(service.ServiceName, ws); err != nil {
		log.Fatalf("Service execution failure: %v", err)
	}
}

func runTrayCommand(args []string) {
	// Set explicit application user model ID for Windows taskbar branding
	appId, _ := syscall.UTF16PtrFromString("PCRemote.App")
	procSetAppUserModelID.Call(uintptr(unsafe.Pointer(appId)))

	fs := flag.NewFlagSet("tray", flag.ExitOnError)
	flagSilent := fs.Bool("silent", false, "Start minimized in system tray without opening QR window")
	_ = fs.Parse(args)

	// Single Instance Lock: Ensure only one tray instance runs per user session
	mutexName, _ := syscall.UTF16PtrFromString("Local\\PCRemote_Tray_SingleInstance_Mutex")
	hMutex, _, errMutex := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	const ERROR_ALREADY_EXISTS = 183
	if errno, ok := errMutex.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
		// Another tray instance is running; restore its pairing window
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

	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	baseDir := filepath.Dir(exePath)

	ipcClient := ipc.NewClient(ipc.PipeName)
	defer ipcClient.Close()

	// Wait up to 3 seconds for service to respond if it was just started
	_ = ipcClient.Connect(3 * time.Second)

	// Execute legacy migration check: decrypt old user DPAPI key and transfer via local pipe to service
	checkAndMigrateLegacyConfig(ipcClient, baseDir)

	// Check device name and PIN status from service
	deviceName := "Windows PC"
	status, err := ipcClient.GetStatus()
	if err == nil && status.DeviceName != "" {
		deviceName = status.DeviceName
	}

	pinStat, err := ipcClient.GetPinStatus()
	if err == nil && !pinStat.IsSet {
		// First-Time Setup: Prompt user to set initial Master PIN
		pin, ok := tray.PromptInitialPinSetup(deviceName)
		if !ok || len(pin) != 6 || !isAllDigits(pin) {
			log.Println("[Setup] Initial PIN setup was cancelled. Exiting tray.")
			return
		}
		if _, err := ipcClient.SetPin("", pin); err != nil {
			log.Printf("[Setup] Failed to save initial PIN: %v", err)
		}
	}

	// Initialize and run interactive Win32 desktop tray manager
	trayMgr := tray.NewManager(ipcClient, deviceName, *flagSilent, nil)
	trayMgr.Run()
}

func checkAndMigrateLegacyConfig(client *ipc.Client, legacyDir string) {
	legacyFile := filepath.Join(legacyDir, "config.json")
	data, err := os.ReadFile(legacyFile)
	if err != nil {
		return // No legacy config file found
	}

	var legacy struct {
		PinHash            string                    `json:"pinHash"`
		PinSalt            string                    `json:"pinSalt"`
		DeviceName         string                    `json:"deviceName"`
		LaptopPrivKeyDPAPI string                    `json:"laptopPrivKeyEncrypted"`
		LaptopPrivKey      string                    `json:"laptopPrivKey"`
		LaptopPubKey       string                    `json:"laptopPubKey"`
		AuthorizedDevices  map[string]ipc.DeviceInfo `json:"authorizedDevices"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		log.Printf("[Migration] Warning: unmarshaling legacy config failed: %v", err)
		return
	}

	var plainKey string
	if legacy.LaptopPrivKeyDPAPI != "" {
		cipherBytes, err := base64.StdEncoding.DecodeString(legacy.LaptopPrivKeyDPAPI)
		if err == nil {
			plainBytes, errDec := config.DecryptDPAPI(cipherBytes)
			if errDec == nil && len(plainBytes) > 0 {
				plainKey = string(plainBytes)
			}
		}
	}
	if plainKey == "" && legacy.LaptopPrivKey != "" {
		plainKey = legacy.LaptopPrivKey
	}

	if plainKey == "" {
		return
	}

	// Verify BIP-340 key validity before transmission
	if _, err := nostr.GetPublicKey(plainKey); err != nil {
		log.Printf("[Migration] Warning: invalid legacy keypair: %v", err)
		return
	}

	params := ipc.MigrateKeyParams{
		PrivateKey:        plainKey,
		PinHash:           legacy.PinHash,
		PinSalt:           legacy.PinSalt,
		DeviceName:        legacy.DeviceName,
		AuthorizedDevices: legacy.AuthorizedDevices,
	}

	// Send over local IPC to service
	res, err := client.MigrateLegacyKey(params)
	if err == nil && res != nil && res.Success {
		log.Println("[Migration] Successfully migrated legacy configuration to background service")
		// Safely rename obsolete file so it is never migrated twice
		migratedFile := legacyFile + ".migrated"
		_ = os.Remove(migratedFile)
		_ = os.Rename(legacyFile, migratedFile)
	} else {
		log.Printf("[Migration] Warning: legacy migration was not confirmed by service; leaving file for retry")
	}
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
