package win32

import (
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modPowrProf = syscall.NewLazyDLL("powrprof.dll")
	modWtsapi32 = syscall.NewLazyDLL("wtsapi32.dll")

	procLockWorkStation              = modUser32.NewProc("LockWorkStation")
	procSetSuspendState              = modPowrProf.NewProc("SetSuspendState")
	procWTSDisconnectSession         = modWtsapi32.NewProc("WTSDisconnectSession")
	procWTSGetActiveConsoleSessionId = modKernel32.NewProc("WTSGetActiveConsoleSessionId")
)

// EnableShutdownPrivilege ensures the calling process token holds SeShutdownPrivilege
func EnableShutdownPrivilege() {
	var token windows.Token
	currentProcess, err := windows.GetCurrentProcess()
	if err != nil {
		return
	}
	err = windows.OpenProcessToken(currentProcess, windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return
	}
	defer token.Close()

	var luid windows.LUID
	namePtr, err := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return
	}
	err = windows.LookupPrivilegeValue(nil, namePtr, &luid)
	if err != nil {
		return
	}

	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{
				Luid:       luid,
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			},
		},
	}
	_ = windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil)
}

// LockScreen immediately locks the Windows workstation, handling both interactive sessions and Session 0
func LockScreen() error {
	SetLockedManually()

	// 1. Attempt standard interactive LockWorkStation
	ret, _, _ := procLockWorkStation.Call()
	if ret != 0 {
		return nil
	}

	// 2. Fallback for Session 0 service context: disconnect the active console session to trigger lock screen
	sessionID, _, _ := procWTSGetActiveConsoleSessionId.Call()
	const invalidSessionID = 0xFFFFFFFF
	if sessionID != invalidSessionID {
		procWTSDisconnectSession.Call(0, sessionID, 0)
	}

	return nil
}

// SuspendSystem puts the PC into low-power sleep mode
func SuspendSystem() error {
	EnableShutdownPrivilege()
	go func() {
		// Small delay to allow the network packet / response to flush out
		time.Sleep(800 * time.Millisecond)
		procSetSuspendState.Call(0, 0, 0)
	}()
	return nil
}

// RebootSystem initiates a system restart in 5 seconds
func RebootSystem() error {
	EnableShutdownPrivilege()
	cmd := exec.Command("shutdown.exe", "/r", "/t", "5", "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// PowerOffSystem initiates a system shutdown in 5 seconds
func PowerOffSystem() error {
	EnableShutdownPrivilege()
	cmd := exec.Command("shutdown.exe", "/s", "/t", "5", "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
