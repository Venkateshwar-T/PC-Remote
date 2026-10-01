package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

const (
	Iterations           = 100000
	MaxFailedAttempts    = 5
	LockoutDuration      = 3 * time.Minute
	DefaultFlushInterval = 30 * time.Second
)

// PBKDF2-SHA256 pure Go implementation without external dependencies
func pbkdf2Sha256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var result []byte
	var blockNumBytes [4]byte

	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		blockNumBytes[0] = byte(block >> 24)
		blockNumBytes[1] = byte(block >> 16)
		blockNumBytes[2] = byte(block >> 8)
		blockNumBytes[3] = byte(block)
		prf.Write(blockNumBytes[:])

		u := prf.Sum(nil)
		blockDigest := make([]byte, len(u))
		copy(blockDigest, u)

		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for k := 0; k < len(blockDigest); k++ {
				blockDigest[k] ^= u[k]
			}
		}

		result = append(result, blockDigest...)
	}

	return result[:keyLen]
}

type DeviceInfo struct {
	Name      string    `json:"name"`
	AddedAt   time.Time `json:"addedAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

type Config struct {
	mu                 sync.RWMutex
	filePath           string
	isDirty            bool
	lastSaveTime       time.Time
	flushInterval      time.Duration
	flushTimer         *time.Timer
	PinHash            string                `json:"pinHash"`
	PinSalt            string                `json:"pinSalt"`
	DeviceName         string                `json:"deviceName"`
	LaptopPrivKeyDPAPI string                `json:"laptopPrivKeyEncrypted,omitempty"`
	LaptopPrivKey      string                `json:"-"` // Stored in-memory only; NEVER written as plaintext to disk
	LaptopPubKey       string                `json:"laptopPubKey"`
	AuthorizedDevices  map[string]DeviceInfo `json:"authorizedDevices"`
	FailedAttempts     int                   `json:"failedAttempts"`
	LockoutUntil       time.Time             `json:"lockoutUntil"`
}

// diskRepresentation is used exclusively during unmarshaling to handle migration from legacy plaintext keys
type diskRepresentation struct {
	PinHash            string                `json:"pinHash"`
	PinSalt            string                `json:"pinSalt"`
	DeviceName         string                `json:"deviceName"`
	LaptopPrivKey      string                `json:"laptopPrivKey,omitempty"` // Legacy plaintext field
	LaptopPrivKeyDPAPI string                `json:"laptopPrivKeyEncrypted,omitempty"`
	LaptopPubKey       string                `json:"laptopPubKey"`
	AuthorizedDevices  map[string]DeviceInfo `json:"authorizedDevices"`
	FailedAttempts     int                   `json:"failedAttempts"`
	LockoutUntil       time.Time             `json:"lockoutUntil"`
}

// LoadOrCreate loads existing config or creates a new one
func LoadOrCreate(baseDir string) (*Config, error) {
	cfgFile := filepath.Join(baseDir, "config.json")
	cfg := &Config{
		filePath:          cfgFile,
		flushInterval:     DefaultFlushInterval,
		AuthorizedDevices: make(map[string]DeviceInfo),
	}

	data, err := os.ReadFile(cfgFile)
	if err == nil {
		var disk diskRepresentation
		if err := json.Unmarshal(data, &disk); err == nil {
			cfg.PinHash = disk.PinHash
			cfg.PinSalt = disk.PinSalt
			cfg.DeviceName = disk.DeviceName
			cfg.LaptopPubKey = disk.LaptopPubKey
			cfg.AuthorizedDevices = disk.AuthorizedDevices
			cfg.FailedAttempts = disk.FailedAttempts
			cfg.LockoutUntil = disk.LockoutUntil
			cfg.LaptopPrivKeyDPAPI = disk.LaptopPrivKeyDPAPI

			if cfg.AuthorizedDevices == nil {
				cfg.AuthorizedDevices = make(map[string]DeviceInfo)
			}

			needsSave := false

			// 1. Decrypt DPAPI key if available
			if disk.LaptopPrivKeyDPAPI != "" {
				cipherBytes, errB64 := base64.StdEncoding.DecodeString(disk.LaptopPrivKeyDPAPI)
				if errB64 == nil {
					plainBytes, errDec := DecryptDPAPI(cipherBytes)
					if errDec == nil && len(plainBytes) > 0 {
						cfg.LaptopPrivKey = string(plainBytes)
					}
				}
			}

			// 2. Migration: If DPAPI was not present or failed, check for legacy plaintext key
			if cfg.LaptopPrivKey == "" && disk.LaptopPrivKey != "" {
				cfg.LaptopPrivKey = disk.LaptopPrivKey
				// Immediately encrypt with DPAPI and mark for disk rewrite
				encBytes, errEnc := EncryptDPAPI([]byte(disk.LaptopPrivKey))
				if errEnc == nil && len(encBytes) > 0 {
					cfg.LaptopPrivKeyDPAPI = base64.StdEncoding.EncodeToString(encBytes)
				}
				needsSave = true
			}

			// 3. Verify cryptographic keypair validity
			if cfg.LaptopPrivKey != "" {
				derivedPub, err := nostr.GetPublicKey(cfg.LaptopPrivKey)
				if err == nil && derivedPub != "" {
					if cfg.LaptopPubKey != derivedPub {
						cfg.LaptopPubKey = derivedPub
						needsSave = true
					}
					if cfg.LaptopPrivKeyDPAPI == "" {
						encBytes, errEnc := EncryptDPAPI([]byte(cfg.LaptopPrivKey))
						if errEnc == nil {
							cfg.LaptopPrivKeyDPAPI = base64.StdEncoding.EncodeToString(encBytes)
							needsSave = true
						}
					}
					if needsSave {
						_ = cfg.Save()
					}
					return cfg, nil
				}
			}
		}
	}

	// Generate default device metadata
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "Windows Laptop"
	}
	cfg.DeviceName = hostname

	// Generate authentic BIP-340 secp256k1 keypair for Nostr identity
	privKey := nostr.GeneratePrivateKey()
	pubKey, err := nostr.GetPublicKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive Nostr public key: %w", err)
	}
	cfg.LaptopPrivKey = privKey
	cfg.LaptopPubKey = pubKey

	// Protect private key with Windows DPAPI
	encBytes, errEnc := EncryptDPAPI([]byte(privKey))
	if errEnc == nil {
		cfg.LaptopPrivKeyDPAPI = base64.StdEncoding.EncodeToString(encBytes)
	}

	if err := cfg.Save(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// HasPin returns true if a valid Master PIN is configured.
func (c *Config) HasPin() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PinHash != "" && c.PinSalt != ""
}

// SetPin updates the master PIN with salt & PBKDF2
func (c *Config) SetPin(pin string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	hash := pbkdf2Sha256([]byte(pin), salt, Iterations, 32)
	c.PinSalt = hex.EncodeToString(salt)
	c.PinHash = hex.EncodeToString(hash)
	c.FailedAttempts = 0
	c.LockoutUntil = time.Time{}

	return c.saveLocked()
}

// VerifyPin validates candidate PIN with constant-time equality check
func (c *Config) VerifyPin(candidate string) (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.PinHash == "" || c.PinSalt == "" {
		return false, "Master PIN has not been configured on this laptop."
	}

	now := time.Now()
	if now.Before(c.LockoutUntil) {
		remaining := int(c.LockoutUntil.Sub(now).Seconds())
		return false, fmt.Sprintf("Too many failed attempts. Locked out for %ds.", remaining)
	}

	salt, err := hex.DecodeString(c.PinSalt)
	if err != nil {
		return false, "Internal configuration error"
	}
	expectedHash, err := hex.DecodeString(c.PinHash)
	if err != nil {
		return false, "Internal configuration error"
	}

	computedHash := pbkdf2Sha256([]byte(candidate), salt, Iterations, 32)

	if subtle.ConstantTimeCompare(expectedHash, computedHash) == 1 {
		c.FailedAttempts = 0
		c.LockoutUntil = time.Time{}
		c.saveLocked()
		return true, ""
	}

	c.FailedAttempts++
	if c.FailedAttempts >= MaxFailedAttempts {
		c.LockoutUntil = now.Add(LockoutDuration)
		c.saveLocked()
		return false, fmt.Sprintf("5 failed attempts. System locked out for %d seconds.", int(LockoutDuration.Seconds()))
	}

	c.saveLocked()
	remainingTries := MaxFailedAttempts - c.FailedAttempts
	return false, fmt.Sprintf("Incorrect PIN. (%d attempts remaining)", remainingTries)
}

// AuthorizeDevice adds or updates an authorized client device
func (c *Config) AuthorizeDevice(pubKey string, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.AuthorizedDevices == nil {
		c.AuthorizedDevices = make(map[string]DeviceInfo)
	}

	c.AuthorizedDevices[pubKey] = DeviceInfo{
		Name:     name,
		AddedAt:  time.Now(),
		LastSeen: time.Now(),
	}
	return c.saveLocked()
}

// RevokeDevice removes an authorized client device
func (c *Config) RevokeDevice(pubKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.AuthorizedDevices != nil {
		delete(c.AuthorizedDevices, pubKey)
		return c.saveLocked()
	}
	return nil
}

// SetFlushInterval overrides the debounce duration for flushing LastSeen updates (useful for tests)
func (c *Config) SetFlushInterval(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushInterval = d
}

// UpdateDeviceLastSeen refreshes the in-memory last active timestamp immediately and debounces disk writes.
func (c *Config) UpdateDeviceLastSeen(pubKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	dev, ok := c.AuthorizedDevices[pubKey]
	if !ok {
		return
	}
	dev.LastSeen = time.Now()
	c.AuthorizedDevices[pubKey] = dev
	c.isDirty = true

	interval := c.flushInterval
	if interval <= 0 {
		interval = DefaultFlushInterval
	}

	// If enough time has elapsed since the last save, flush immediately
	if time.Since(c.lastSaveTime) >= interval {
		if c.flushTimer != nil {
			c.flushTimer.Stop()
			c.flushTimer = nil
		}
		_ = c.saveLocked()
		return
	}

	// Schedule a debounce timer if not already pending
	if c.flushTimer == nil {
		remaining := interval - time.Since(c.lastSaveTime)
		if remaining <= 0 {
			remaining = interval
		}
		c.flushTimer = time.AfterFunc(remaining, func() {
			_ = c.Flush()
		})
	}
}

// Flush synchronously writes any pending dirty configuration to disk.
func (c *Config) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.flushTimer != nil {
		c.flushTimer.Stop()
		c.flushTimer = nil
	}

	if !c.isDirty {
		return nil
	}

	return c.saveLocked()
}

// Close flushes any pending writes and cancels any active timer.
func (c *Config) Close() error {
	return c.Flush()
}

// IsDeviceAuthorized strictly checks if a pubkey is explicitly authorized
func (c *Config) IsDeviceAuthorized(pubKey string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.AuthorizedDevices == nil || len(c.AuthorizedDevices) == 0 {
		return false
	}
	_, ok := c.AuthorizedDevices[pubKey]
	return ok
}

// Save persists config to file
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	if c.flushTimer != nil {
		c.flushTimer.Stop()
		c.flushTimer = nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(c.filePath, data, 0600)
	if err == nil {
		c.isDirty = false
		c.lastSaveTime = time.Now()
	}
	return err
}

// GetServiceConfigDir returns the machine-wide directory for PC Remote service state
func GetServiceConfigDir() string {
	progData := os.Getenv("ProgramData")
	if progData == "" {
		progData = `C:\ProgramData`
	}
	return filepath.Join(progData, "PC Remote")
}

// GetServiceConfigPath returns the absolute path to the service configuration file
func GetServiceConfigPath() string {
	return filepath.Join(GetServiceConfigDir(), "config.json")
}

// SetupDirectorySecurity ensures the specified directory exists and has restricted ACLs
func SetupDirectorySecurity(dir string) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	// Only apply machine ACLs when managing the authoritative service config directory
	if strings.EqualFold(filepath.Clean(dir), filepath.Clean(GetServiceConfigDir())) {
		cmd := exec.Command("icacls.exe", dir, "/grant:r", "SYSTEM:(OI)(CI)F", "Administrators:(OI)(CI)F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Run()
	}
	return nil
}

// MigrateKey securely updates the private key in-memory, re-encrypts with DPAPI, and saves to disk
func (c *Config) MigrateKey(privateKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	derivedPub, err := nostr.GetPublicKey(privateKey)
	if err != nil {
		return fmt.Errorf("invalid Nostr private key: %w", err)
	}

	encBytes, err := EncryptDPAPI([]byte(privateKey))
	if err != nil {
		return fmt.Errorf("failed to encrypt private key with service DPAPI: %w", err)
	}

	c.LaptopPrivKey = privateKey
	c.LaptopPubKey = derivedPub
	c.LaptopPrivKeyDPAPI = base64.StdEncoding.EncodeToString(encBytes)
	return c.saveLocked()
}

