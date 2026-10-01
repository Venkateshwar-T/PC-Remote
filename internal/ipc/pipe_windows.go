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

// Default SDDL security descriptor:
// SY = NT AUTHORITY\SYSTEM (Allow Read/Write)
// BA = BUILTIN\Administrators (Allow Read/Write)
// IU = NT AUTHORITY\INTERACTIVE (Allow Read/Write - active interactive user session)
// NU = NT AUTHORITY\NETWORK (Deny All - blocks network logons)
const DefaultSDDL = "D:(A;;GRGW;;;SY)(A;;GRGW;;;BA)(A;;GRGW;;;IU)(D;;GA;;;NU)"

// PipeConn wraps a Windows named pipe handle as a stream connection
type PipeConn struct {
	handle windows.Handle
	file   *os.File
	mu     sync.Mutex
	closed bool
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
	name      string
	namePtr   *uint16
	sa        *windows.SecurityAttributes
	mu        sync.Mutex
	closed    bool
	curHandle windows.Handle
}

// ListenPipe creates a named pipe listener with strict SDDL and remote network rejection
func ListenPipe(pipeName string) (*PipeListener, error) {
	namePtr, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("invalid pipe name %q: %w", pipeName, err)
	}

	sd, err := windows.SecurityDescriptorFromString(DefaultSDDL)
	if err != nil {
		return nil, fmt.Errorf("failed to create security descriptor from SDDL: %w", err)
	}

	sa := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}

	return &PipeListener{
		name:    pipeName,
		namePtr: namePtr,
		sa:      sa,
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
	l.mu.Unlock()

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
