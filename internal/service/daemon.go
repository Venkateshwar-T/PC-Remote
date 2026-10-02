package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"laptopcontrol/internal/config"
	"laptopcontrol/internal/ipc"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/protocol"
	"laptopcontrol/internal/relay"
	"laptopcontrol/internal/remote"
	"laptopcontrol/internal/remote/session"
	"laptopcontrol/internal/server"
	"laptopcontrol/internal/win32"
	"laptopcontrol/web"
)

// Daemon coordinates all background operations for PC Remote
type Daemon struct {
	port        int
	pipeName    string
	configDir   string
	cfg         *config.Config
	pairMgr     *pairing.Manager
	proto       *protocol.Handler
	remoteMgr   *remote.SessionManager
	localSrv    *server.Server
	httpServer  *http.Server
	relayClient *relay.Client
	ipcServer   *ipc.Server
	stopChan    chan struct{}
	stopOnce    sync.Once
	startTime   time.Time
}

// NewDaemon initializes a new headless service daemon
func NewDaemon(port int, pipeName, configDir string) *Daemon {
	if port <= 0 {
		port = 8765
	}
	if pipeName == "" {
		pipeName = ipc.PipeName
	}
	if configDir == "" {
		configDir = config.GetServiceConfigDir()
	}
	return &Daemon{
		port:      port,
		pipeName:  pipeName,
		configDir: configDir,
		stopChan:  make(chan struct{}),
		startTime: time.Now(),
	}
}

// Start boots all headless daemon subsystems (Config, Crypto, LAN, Relay, IPC)
func (d *Daemon) Start() error {
	log.Printf("[Daemon] Initializing service daemon in %s", d.configDir)

	// 1. Secure configuration directory (fail-closed if permissions cannot be enforced/verified)
	if err := config.SetupDirectorySecurity(d.configDir); err != nil {
		return fmt.Errorf("failed to secure service configuration directory: %w", err)
	}

	// 2. Load or initialize machine-wide protected configuration
	cfg, err := config.LoadOrCreate(d.configDir)
	if err != nil {
		return fmt.Errorf("failed to load service configuration: %w", err)
	}
	d.cfg = cfg

	// 3. Initialize fail-closed Pairing Manager
	pairMgr, err := pairing.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize pairing manager: %w", err)
	}
	d.pairMgr = pairMgr

	// 4. Initialize protocol handler & replay protection
	replayGuard := protocol.NewReplayGuard(120)
	protoHandler := protocol.NewHandler(cfg, pairMgr, replayGuard)
	d.proto = protoHandler

	// 5. Initialize Remote Desktop Session Manager
	remoteMgr := remote.NewSessionManager(cfg)
	d.remoteMgr = remoteMgr
	protoHandler.SetRemoteManager(remoteMgr)

	// 6. Initialize LAN HTTP Server
	localSrv := server.NewServer(cfg, web.Assets, d.port, pairMgr, protoHandler)
	d.localSrv = localSrv
	d.httpServer = &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", d.port),
		Handler:      localSrv,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// 7. Initialize Nostr Relay Client
	relayClient := relay.NewClient(cfg, nil, protoHandler)
	d.relayClient = relayClient

	// Wire outbound Remote Desktop WebRTC signaling publisher over Nostr relay pool
	remoteMgr.SetOutboundSignalHandler(func(phonePubKey string, packet session.SignalingPacket) {
		respEvt, err := protoHandler.BuildEncryptedResponsePacket(phonePubKey, protocol.ResponsePacket{
			ID:        fmt.Sprintf("sig_%d", time.Now().UnixNano()),
			Status:    "ok",
			Signal:    &packet,
			Timestamp: time.Now().Unix(),
		})
		if err == nil && respEvt != nil && d.relayClient != nil {
			d.relayClient.PublishEvent(respEvt)
		}
	})

	// 7. Configure IPC Handlers for communication with interactive desktop tray
	ipcHandlers := ipc.Handlers{
		GetStatus: func() (*ipc.StatusResult, error) {
			return &ipc.StatusResult{
				Running:     true,
				Version:     "2.0.0",
				DeviceName:  d.cfg.DeviceName,
				PairedCount: len(d.cfg.AuthorizedDevices),
				Port:        d.port,
				Uptime:      win32.GetFormattedUptime(),
			}, nil
		},
		GetPairingInfo: func() (*ipc.PairingInfoResult, error) {
			return &ipc.PairingInfoResult{
				URL:          d.localSrv.GetPairingURL(),
				Token:        d.pairMgr.GetToken(),
				ExpiryUnix:   d.pairMgr.GetExpiresAt().Unix(),
				LaptopPubKey: d.cfg.LaptopPubKey,
			}, nil
		},
		RotatePairingToken: func() (*ipc.PairingInfoResult, error) {
			newToken, err := d.pairMgr.Regenerate()
			if err != nil {
				return nil, fmt.Errorf("failed to rotate pairing token: %w", err)
			}
			return &ipc.PairingInfoResult{
				URL:          d.localSrv.GetPairingURL(),
				Token:        newToken,
				ExpiryUnix:   d.pairMgr.GetExpiresAt().Unix(),
				LaptopPubKey: d.cfg.LaptopPubKey,
			}, nil
		},
		GetPinStatus: func() (*ipc.PinStatusResult, error) {
			return &ipc.PinStatusResult{
				IsSet: d.cfg.HasPin(),
			}, nil
		},
		SetPin: func(oldPin, newPin string) (*ipc.SetPinResult, error) {
			if len(newPin) != 6 {
				return nil, fmt.Errorf("Master PIN must be exactly 6 digits")
			}
			if d.cfg.HasPin() {
				valid, msg := d.cfg.VerifyPin(oldPin)
				if !valid {
					return nil, fmt.Errorf("incorrect current Master PIN: %s", msg)
				}
			}
			if err := d.cfg.SetPin(newPin); err != nil {
				return nil, fmt.Errorf("failed to save Master PIN: %w", err)
			}
			return &ipc.SetPinResult{Success: true, Message: "Master PIN updated successfully"}, nil
		},
		MigrateLegacyKey: func(params ipc.MigrateKeyParams) (*ipc.MigrateKeyResult, error) {
			devices := make(map[string]config.DeviceInfo)
			for k, v := range params.AuthorizedDevices {
				devices[k] = config.DeviceInfo{
					Name:     v.Name,
					AddedAt:  v.AddedAt,
					LastSeen: v.LastSeen,
				}
			}
			data := config.LegacyConfigData{
				PrivateKey:        params.PrivateKey,
				PinHash:           params.PinHash,
				PinSalt:           params.PinSalt,
				DeviceName:        params.DeviceName,
				AuthorizedDevices: devices,
			}
			if err := d.cfg.MigrateLegacyConfig(data); err != nil {
				return nil, fmt.Errorf("migration rejected: %w", err)
			}
			log.Println("[Daemon] Successfully imported, verified, and protected legacy configuration under service")
			return &ipc.MigrateKeyResult{Success: true, Message: "Legacy configuration migrated successfully"}, nil
		},
		StopService: func() (*ipc.StopServiceResult, error) {
			go func() {
				time.Sleep(100 * time.Millisecond)
				d.Stop()
			}()
			return &ipc.StopServiceResult{Success: true}, nil
		},
	}

	// Determine the authorized Windows user SID for named pipe ACL and server identity validation
	userSID := d.cfg.AuthorizedUserSID
	if userSID == "" {
		if consoleSID, err := ipc.GetActiveConsoleUserSID(); err == nil && consoleSID != "" {
			userSID = consoleSID
			d.cfg.AuthorizedUserSID = consoleSID
			_ = d.cfg.Save()
			log.Printf("[Daemon] Pinned authorized local user SID: %s", userSID)
		}
	}

	d.ipcServer = ipc.NewServer(d.pipeName, ipcHandlers, userSID)

	// 8. Launch listeners
	go func() {
		log.Printf("[Daemon] LAN Server starting on port %d", d.port)
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Daemon] LAN Server error: %v", err)
		}
	}()

	log.Println("[Daemon] Starting Nostr relay connections")
	d.relayClient.Start()

	log.Printf("[Daemon] Starting Local Named Pipe IPC server at %s", d.pipeName)
	if err := d.ipcServer.Start(); err != nil {
		return fmt.Errorf("failed to start IPC server: %w", err)
	}

	log.Println("[Daemon] Service daemon running successfully")
	go func() {
		time.Sleep(1 * time.Second)
		win32.TrimProcessMemory()
	}()
	return nil
}

// Stop cleanly flushes config and halts all listeners
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		log.Println("[Daemon] Shutting down service daemon...")
		close(d.stopChan)

		if d.ipcServer != nil {
			_ = d.ipcServer.Stop()
		}

		if d.httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = d.httpServer.Shutdown(ctx)
		}

		if d.relayClient != nil {
			d.relayClient.Stop()
		}

		if d.remoteMgr != nil {
			d.remoteMgr.Close()
		}

		if d.cfg != nil {
			_ = d.cfg.Flush()
			_ = d.cfg.Close()
		}

		log.Println("[Daemon] Shutdown complete")
	})
}

// Wait blocks until the daemon is stopped
func (d *Daemon) Wait() {
	<-d.stopChan
}
