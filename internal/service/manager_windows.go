//go:build windows

package service

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	ServiceName        = "PCRemote"
	ServiceDisplayName = "PC Remote Background Service"
	ServiceDescription = "Allows your PC to remain remotely reachable after reboot, even before you sign in to Windows."
)

// InstallService registers the PC Remote service with Windows SCM
func InstallService(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to Service Control Manager (are you running as administrator?): %w", err)
	}
	defer m.Disconnect()

	// Check if already installed
	s, err := m.OpenService(ServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s is already installed", ServiceName)
	}

	absPath, err := filepath.Abs(exePath)
	if err != nil {
		absPath = exePath
	}

	// Create service configured with LocalSystem account, Auto-Start, and explicit description
	serviceConfig := mgr.Config{
		ServiceType:    windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:      mgr.StartAutomatic,
		ErrorControl:   mgr.ErrorNormal,
		DisplayName:    ServiceDisplayName,
		Description:    ServiceDescription,
		BinaryPathName: fmt.Sprintf(`"%s" service`, absPath),
	}

	s, err = m.CreateService(ServiceName, absPath, serviceConfig, "service")
	if err != nil {
		return fmt.Errorf("failed to create service %s: %w", ServiceName, err)
	}
	defer s.Close()

	// Configure SCM Service Recovery: Restart on failure (5s, 10s, 30s; reset fail count after 1 day)
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	const oneDaySeconds = uint32(86400)
	if err := s.SetRecoveryActions(recoveryActions, oneDaySeconds); err != nil {
		log.Printf("[Service Manager] Warning: could not set recovery actions: %v", err)
	}

	log.Printf("[Service Manager] Service %s successfully installed (SERVICE_AUTO_START)", ServiceName)
	return nil
}

// UninstallService stops and deletes the service from Windows SCM
func UninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to Service Control Manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", ServiceName, err)
	}
	defer s.Close()

	// Stop service first if running
	status, err := s.Query()
	if err == nil && status.State != svc.Stopped {
		log.Println("[Service Manager] Stopping service before removal...")
		_, _ = s.Control(svc.Stop)
		// Wait up to 5 seconds for STOPPED state
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			status, err = s.Query()
			if err == nil && status.State == svc.Stopped {
				break
			}
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	log.Printf("[Service Manager] Service %s successfully removed from SCM", ServiceName)
	return nil
}

// StartService starts the registered Windows service
func StartService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", ServiceName, err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	log.Printf("[Service Manager] Service %s start command dispatched", ServiceName)
	return nil
}

// StopService stops the running Windows service
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s not found: %w", ServiceName, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	log.Printf("[Service Manager] Stop signaled, current state: %d", status.State)
	return nil
}

// QueryStatus returns the current SCM status of the service
func QueryStatus() (svc.Status, error) {
	m, err := mgr.Connect()
	if err != nil {
		return svc.Status{}, fmt.Errorf("failed to connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return svc.Status{}, fmt.Errorf("service %s not found: %w", ServiceName, err)
	}
	defer s.Close()

	return s.Query()
}
