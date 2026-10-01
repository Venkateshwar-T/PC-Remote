//go:build !windows

package service

import "errors"

const (
	ServiceName        = "PCRemote"
	ServiceDisplayName = "PC Remote Background Service"
	ServiceDescription = "Allows your PC to remain remotely reachable after reboot, even before you sign in to Windows."
)

var errWindowsOnly = errors.New("Windows Service Control Manager is only available on Windows")

type WindowsService struct{}

func NewWindowsService(port int, pipeName, configDir string) *WindowsService {
	return &WindowsService{}
}

func RunAsService(name string, ws *WindowsService) error {
	return errWindowsOnly
}

func IsServiceSession() (bool, error) {
	return false, nil
}

func InstallService(exePath string) error {
	return errWindowsOnly
}

func UninstallService() error {
	return errWindowsOnly
}

func StartService() error {
	return errWindowsOnly
}

func StopService() error {
	return errWindowsOnly
}
