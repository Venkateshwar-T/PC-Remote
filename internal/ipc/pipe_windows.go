//go:build windows

package ipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modadvapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procImpersonateNamedPipeClient = modadvapi32.NewProc("ImpersonateNamedPipeClient")
	procRevertToSelf               = modadvapi32.NewProc("RevertToSelf")
)

// BuildRestrictedSDDL generates a hardened SDDL restricting pipe access to SYSTEM,
// Administrators, and optionally an authorized user SID, while explicitly denying Network access.
// Note: Interactive Users (IU) is permanently removed to prevent unauthorized local accounts from accessing privileged IPC.
func BuildRestrictedSDDL(userSID string) string {
	if userSID != "" {
		return fmt.Sprintf("D:(A;;GRGW;;;SY)(A;;GRGW;;;BA)(A;;GRGW;;;%s)(D;;GA;;;NU)", userSID)
	}
	return "D:(A;;GRGW;;;SY)(A;;GRGW;;;BA)(D;;GA;;;NU)"
}

// GetActiveConsoleUserSID retrieves the Windows SID of the currently logged-in console session user.
func GetActiveConsoleUserSID() (string, error) {
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0xFFFFFFFF {
		return "", fmt.Errorf("no active console session")
	}
	var token windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
		return "", fmt.Errorf("WTSQueryUserToken failed for session %d: %w", sessionID, err)
	}
	defer token.Close()

	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve token user info: %w", err)
	}
	return tokenUser.User.Sid.String(), nil
}

// GetCurrentProcessUserSID retrieves the Windows SID of the current process user.
func GetCurrentProcessUserSID() (string, error) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return "", fmt.Errorf("OpenProcessToken failed: %w", err)
	}
	defer token.Close()

	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve token user info: %w", err)
	}
	return tokenUser.User.Sid.String(), nil
}

// ClientIdentity captures the Windows security identity of a connected pipe client.
type ClientIdentity struct {
	SID      string
	IsAdmin  bool
	IsSystem bool
}

// GetClientIdentity impersonates the connecting client on the named pipe, inspects its token, and reverts.
func GetClientIdentity(pipeHandle windows.Handle) (*ClientIdentity, error) {
	r1, _, err := procImpersonateNamedPipeClient.Call(uintptr(pipeHandle))
	if r1 == 0 {
		return nil, fmt.Errorf("ImpersonateNamedPipeClient failed: %w", err)
	}
	defer procRevertToSelf.Call()

	var threadToken windows.Token
	err = windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &threadToken)
	if err != nil {
		return nil, fmt.Errorf("OpenThreadToken failed: %w", err)
	}
	defer threadToken.Close()

	tokenUser, err := threadToken.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("GetTokenUser failed: %w", err)
	}

	clientSID := tokenUser.User.Sid.String()

	adminSid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	isAdmin := false
	if err == nil {
		isAdmin, _ = threadToken.IsMember(adminSid)
	}

	systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	isSystem := false
	if err == nil {
		isSystem, _ = threadToken.IsMember(systemSid)
	}

	return &ClientIdentity{
		SID:      clientSID,
		IsAdmin:  isAdmin,
		IsSystem: isSystem,
	}, nil
}

// PipeConn wraps a Windows named pipe handle as a stream connection
type PipeConn struct {
	handle windows.Handle
	file   *os.File
	mu     sync.Mutex
	closed bool
}

func (c *PipeConn) Handle() windows.Handle {
	return c.handle
}

func (c *PipeConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	f := c.file
	c.mu.Unlock()
	return f.Read(b)
}

func (c *PipeConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	f := c.file
	c.mu.Unlock()
	return f.Write(b)
}

func (c *PipeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.file != nil {
		_ = c.file.Close()
	}
	_ = windows.DisconnectNamedPipe(c.handle)
	_ = windows.CloseHandle(c.handle)
	return nil
}

// PipeListener manages accepting connections on a Windows Named Pipe
type PipeListener struct {
	name          string
	namePtr       *uint16
	sa            *windows.SecurityAttributes
	mu            sync.Mutex
	closed        bool
	curHandle     windows.Handle
	authorizedSID string
}

// ListenPipe creates a named pipe listener with hardened SDDL and remote network rejection.
// If authorizedSID is not provided, it attempts resolution via active console user or current process.
func ListenPipe(pipeName string, authorizedSID ...string) (*PipeListener, error) {
	namePtr, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe name %q: %w", pipeName, err)
	}

	var userSID string
	if len(authorizedSID) > 0 && authorizedSID[0] != "" {
		userSID = authorizedSID[0]
	} else {
		if sid, err := GetActiveConsoleUserSID(); err == nil && sid != "" {
			userSID = sid
		} else if sid, err := GetCurrentProcessUserSID(); err == nil && sid != "" {
			userSID = sid
		}
	}

	sddl := BuildRestrictedSDDL(userSID)
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("failed to create security descriptor from SDDL (%s): %w", sddl, err)
	}

	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}

	return &PipeListener{
		name:          pipeName,
		namePtr:       namePtr,
		sa:            sa,
		authorizedSID: userSID,
	}, nil
}

// Accept waits for and returns the next connection to the named pipe
func (l *PipeListener) Accept() (*PipeConn, error) {
	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return nil, net.ErrClosed
		}

		hPipe, err := windows.CreateNamedPipe(
			l.namePtr,
			windows.PIPE_ACCESS_DUPLEX,
			windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
			windows.PIPE_UNLIMITED_INSTANCES,
			65536,
			65536,
			5000,
			l.sa,
		)
		if err != nil {
			l.mu.Unlock()
			return nil, fmt.Errorf("CreateNamedPipe failed: %w", err)
		}
		l.curHandle = hPipe
		l.mu.Unlock()

		// Wait for client connection
		connErr := windows.ConnectNamedPipe(hPipe, nil)
		if connErr == windows.ERROR_PIPE_CONNECTED {
			connErr = nil
		}

		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			_ = windows.DisconnectNamedPipe(hPipe)
			_ = windows.CloseHandle(hPipe)
			return nil, net.ErrClosed
		}
		l.curHandle = 0
		l.mu.Unlock()

		if connErr != nil {
			_ = windows.CloseHandle(hPipe)
			continue
		}

		// Connected successfully
		return &PipeConn{
			handle: hPipe,
			file:   os.NewFile(uintptr(hPipe), "pipe-server"),
		}, nil
	}
}

// Close terminates the pipe listener and wakes any blocked Accept() call
func (l *PipeListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	h := l.curHandle
	l.mu.Unlock()

	if h != 0 {
		_ = windows.CancelIoEx(h, nil)
	}

	// Connect dummy client to release blocked ConnectNamedPipe call immediately
	hWake, err := windows.CreateFile(
		l.namePtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err == nil {
		_ = windows.CloseHandle(hWake)
	}

	return nil
}

// Dial connects to a named pipe with timeout and retries
func Dial(pipeName string, timeout time.Duration) (*PipeConn, error) {
	namePtr, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe name %q: %w", pipeName, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		hClient, err := windows.CreateFile(
			namePtr,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			return &PipeConn{
				handle: hClient,
				file:   os.NewFile(uintptr(hClient), "pipe-client"),
			}, nil
		}

		// If pipe doesn't exist yet or all instances are busy, retry until deadline
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to connect to pipe %s: %w", pipeName, err)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// IsPipeNotFoundError checks if an error indicates the service pipe is not running
func IsPipeNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, os.ErrNotExist)
}
