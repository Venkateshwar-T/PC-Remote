//go:build !windows

package ipc

import (
	"errors"
	"time"
)

type SessionPipeServer struct{}

func NewSessionPipeServer(pipeName, sessionID, userSID string) (*SessionPipeServer, error) {
	return nil, errors.New("named pipes only supported on Windows")
}
func (s *SessionPipeServer) SetOnMessage(fn func(env *AgentEnvelope)) {}
func (s *SessionPipeServer) SetOnClientClosed(fn func())               {}
func (s *SessionPipeServer) Accept() error                            { return errors.New("not supported") }
func (s *SessionPipeServer) Send(msgType AgentMessageType, sessionID string, payload interface{}) error {
	return errors.New("not supported")
}
func (s *SessionPipeServer) StartReadLoop() error { return errors.New("not supported") }
func (s *SessionPipeServer) Close() error { return nil }

type SessionPipeClient struct{}

func ConnectSessionPipe(pipeName, sessionID string, timeout time.Duration) (*SessionPipeClient, error) {
	return nil, errors.New("named pipes only supported on Windows")
}
func (c *SessionPipeClient) SetOnMessage(fn func(env *AgentEnvelope)) {}
func (c *SessionPipeClient) StartReadLoop() error                      { return errors.New("not supported") }
func (c *SessionPipeClient) Send(msgType AgentMessageType, sessionID string, payload interface{}) error {
	return errors.New("not supported")
}
func (c *SessionPipeClient) Close() error { return nil }
