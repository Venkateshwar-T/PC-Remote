//go:build !windows

package ipc

import (
	"errors"
	"net"
	"time"
)

var errNotSupported = errors.New("named pipes only supported on Windows")

type PipeConn struct {
	net.Conn
}

type PipeListener struct {
	closed bool
}

func ListenPipe(pipeName string) (*PipeListener, error) {
	return nil, errNotSupported
}

func (l *PipeListener) Accept() (*PipeConn, error) {
	return nil, errNotSupported
}

func (l *PipeListener) Close() error {
	return nil
}

func Dial(pipeName string, timeout time.Duration) (*PipeConn, error) {
	return nil, errNotSupported
}

func IsPipeNotFoundError(err error) bool {
	return false
}
