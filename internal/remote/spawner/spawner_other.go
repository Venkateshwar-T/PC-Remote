//go:build !windows

package spawner

import (
	"errors"
	"time"
)

type ProcessHandle struct{}

func (p *ProcessHandle) Terminate()                  {}
func (p *ProcessHandle) IsRunning() bool             { return false }
func (p *ProcessHandle) WaitForExit(d time.Duration) bool { return true }
func (p *ProcessHandle) GetExitCode() (uint32, bool) { return 0, false }
func (p *ProcessHandle) PID() uint32                 { return 0 }

type Spawner struct{}

func NewSpawner() *Spawner {
	return &Spawner{}
}

func (s *Spawner) SpawnSessionAgent(sessionID, pipeName, authChallenge, phonePubKey string) (*ProcessHandle, error) {
	return nil, errors.New("interactive session spawner is only supported on Windows")
}
