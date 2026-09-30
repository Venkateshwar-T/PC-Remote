package win32

import (
	"os/exec"
	"syscall"
	"time"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modPowrProf = syscall.NewLazyDLL("powrprof.dll")

	procLockWorkStation = modUser32.NewProc("LockWorkStation")
	procSetSuspendState = modPowrProf.NewProc("SetSuspendState")
)

// LockScreen immediately locks the Windows workstation
func LockScreen() error {
	SetLockedManually()
	procLockWorkStation.Call()
	return nil
}

// SuspendSystem puts the PC into low-power sleep mode
func SuspendSystem() error {
	go func() {
		// Small delay to allow the network packet / response to flush out
		time.Sleep(800 * time.Millisecond)
		procSetSuspendState.Call(0, 0, 0)
	}()
	return nil
}

// RebootSystem initiates a system restart in 5 seconds
func RebootSystem() error {
	cmd := exec.Command("shutdown.exe", "/r", "/t", "5", "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// PowerOffSystem initiates a system shutdown in 5 seconds
func PowerOffSystem() error {
	cmd := exec.Command("shutdown.exe", "/s", "/t", "5", "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
