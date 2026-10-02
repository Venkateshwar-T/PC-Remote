//go:build windows

package spawner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUserenv                 = windows.NewLazySystemDLL("userenv.dll")
	procCreateEnvironmentBlock  = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = modUserenv.NewProc("DestroyEnvironmentBlock")
)

// ProcessHandle wraps the spawned process information.
type ProcessHandle struct {
	ProcessHandle windows.Handle
	ThreadHandle  windows.Handle
	ProcessID     uint32
	SessionID     uint32
	mu            sync.Mutex
	terminated    bool
}

// Spawner handles launching the session-agent worker into the interactive console desktop.
type Spawner struct{}

// NewSpawner creates a new session spawner.
func NewSpawner() *Spawner {
	return &Spawner{}
}

// SpawnSessionAgent launches PC-Remote.exe in session-agent mode inside the active console session.
func (s *Spawner) SpawnSessionAgent(sessionID, pipeName, authChallenge, phonePubKey string) (*ProcessHandle, error) {
	consoleSessionID := windows.WTSGetActiveConsoleSessionId()
	if consoleSessionID == 0xFFFFFFFF {
		return nil, fmt.Errorf("no active interactive console session found (system may be logged out or at secure desktop)")
	}

	var userToken windows.Token
	err := windows.WTSQueryUserToken(consoleSessionID, &userToken)
	if err != nil {
		return nil, fmt.Errorf("failed to query user token for console session %d: %w", consoleSessionID, err)
	}
	defer userToken.Close()

	// Duplicate token as primary token for process creation
	var primaryToken windows.Token
	err = windows.DuplicateTokenEx(
		userToken,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primaryToken,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to duplicate console user token: %w", err)
	}
	defer primaryToken.Close()

	// Create environment block for target user
	var envBlock uintptr
	r1, _, errEnv := procCreateEnvironmentBlock.Call(
		uintptr(unsafe.Pointer(&envBlock)),
		uintptr(primaryToken),
		0, // inheritFromCurrentProcess = FALSE
	)
	if r1 == 0 {
		log.Printf("[Spawner] Warning: CreateEnvironmentBlock failed: %v", errEnv)
		envBlock = 0
	} else {
		defer procDestroyEnvironmentBlock.Call(envBlock)
	}

	// Resolve executable path
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve current executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	desktop, err := windows.UTF16PtrFromString("winsta0\\default")
	if err != nil {
		return nil, err
	}

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop = desktop
	si.Flags = windows.STARTF_USESHOWWINDOW
	si.ShowWindow = windows.SW_HIDE

	// Command line string
	cmdLine := fmt.Sprintf(
		"\"%s\" session-agent --session-id=\"%s\" --pipe=\"%s\" --challenge=\"%s\" --phone-pubkey=\"%s\"",
		exePath,
		sessionID,
		pipeName,
		authChallenge,
		phonePubKey,
	)
	cmdLineUTF16, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return nil, fmt.Errorf("invalid command line encoding: %w", err)
	}

	appPathUTF16, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return nil, err
	}

	curDirUTF16, err := windows.UTF16PtrFromString(exeDir)
	if err != nil {
		return nil, err
	}

	creationFlags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NO_WINDOW)

	var pi windows.ProcessInformation
	err = windows.CreateProcessAsUser(
		primaryToken,
		appPathUTF16,
		cmdLineUTF16,
		nil,
		nil,
		false,
		creationFlags,
		(*uint16)(unsafe.Pointer(envBlock)),
		curDirUTF16,
		&si,
		&pi,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateProcessAsUser failed for console session %d: %w", consoleSessionID, err)
	}

	log.Printf("[Spawner] Successfully spawned session-agent (PID: %d) into interactive session %d", pi.ProcessId, consoleSessionID)

	return &ProcessHandle{
		ProcessHandle: pi.Process,
		ThreadHandle:  pi.Thread,
		ProcessID:     pi.ProcessId,
		SessionID:     consoleSessionID,
	}, nil
}

// Terminate stops the worker process and releases Windows handles.
func (p *ProcessHandle) Terminate() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.terminated {
		return
	}
	p.terminated = true

	if p.ProcessHandle != 0 {
		_ = windows.TerminateProcess(p.ProcessHandle, 1)
		_, _ = windows.WaitForSingleObject(p.ProcessHandle, 1000)
		_ = windows.CloseHandle(p.ProcessHandle)
		p.ProcessHandle = 0
	}
	if p.ThreadHandle != 0 {
		_ = windows.CloseHandle(p.ThreadHandle)
		p.ThreadHandle = 0
	}
	log.Printf("[Spawner] Cleaned up session-agent PID %d", p.ProcessID)
}

// IsRunning checks if the spawned process is still executing.
func (p *ProcessHandle) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.terminated || p.ProcessHandle == 0 {
		return false
	}
	var exitCode uint32
	err := windows.GetExitCodeProcess(p.ProcessHandle, &exitCode)
	if err != nil {
		return false
	}
	const STILL_ACTIVE = 259
	return exitCode == STILL_ACTIVE
}

// WaitForExit waits up to timeout for the process to exit.
func (p *ProcessHandle) WaitForExit(timeout time.Duration) bool {
	p.mu.Lock()
	h := p.ProcessHandle
	p.mu.Unlock()

	if h == 0 {
		return true
	}
	millis := uint32(timeout.Milliseconds())
	res, _ := windows.WaitForSingleObject(h, millis)
	return res == windows.WAIT_OBJECT_0
}
