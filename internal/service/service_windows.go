//go:build windows

package service

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

// WindowsService implements the SCM Handler interface
type WindowsService struct {
	port      int
	pipeName  string
	configDir string
	daemon    *Daemon
}

// NewWindowsService constructs an SCM service handler
func NewWindowsService(port int, pipeName, configDir string) *WindowsService {
	return &WindowsService{
		port:      port,
		pipeName:  pipeName,
		configDir: configDir,
	}
}

// Execute is called by Windows SCM when the service is started
func (ws *WindowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	daemon := NewDaemon(ws.port, ws.pipeName, ws.configDir)
	ws.daemon = daemon

	if err := daemon.Start(); err != nil {
		log.Printf("[Service] Failed to start daemon: %v", err)
		return false, 1
	}

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	log.Println("[Service] SCM: Transitioned to RUNNING state")

	for req := range r {
		switch req.Cmd {
		case svc.Interrogate:
			changes <- req.CurrentStatus
		case svc.Stop, svc.Shutdown:
			log.Println("[Service] SCM: Stop/Shutdown request received")
			changes <- svc.Status{State: svc.StopPending}
			daemon.Stop()
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		default:
			log.Printf("[Service] SCM: Unexpected control request #%d", req.Cmd)
		}
	}

	return false, 0
}

// RunAsService executes the service within the Windows Service Control Manager environment
func RunAsService(name string, ws *WindowsService) error {
	return svc.Run(name, ws)
}

// IsServiceSession detects whether the current process is running inside Windows SCM
func IsServiceSession() (bool, error) {
	return svc.IsWindowsService()
}
