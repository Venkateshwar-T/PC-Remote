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

type ClientIdentity struct {
	SID      string
	IsAdmin  bool
	IsSystem bool
}

func BuildRestrictedSDDL(userSID string) string {
	return ""
}

func GetActiveConsoleUserSID() (string, error) {
	return "", errNotSupported
}

func GetCurrentProcessUserSID() (string, error) {
	return "", errNotSupported
}

func GetClientIdentity(h interface{}) (*ClientIdentity, error) {
	return &ClientIdentity{IsAdmin: true, IsSystem: true}, nil
}

func ListenPipe(pipeName string, authorizedSID ...string) (*PipeListener, error) {
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
