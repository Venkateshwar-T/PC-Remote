package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

const (
	Iterations        = 100000
	MaxFailedAttempts = 5
	LockoutDuration   = 3 * time.Minute
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
	mu                sync.RWMutex
	filePath          string
	PinHash           string                `json:"pinHash"`
	PinSalt           string                `json:"pinSalt"`
	DeviceName        string                `json:"deviceName"`
	LaptopPrivKey     string                `json:"laptopPrivKey"`
	LaptopPubKey      string                `json:"laptopPubKey"`
	AuthorizedDevices map[string]DeviceInfo `json:"authorizedDevices"`
	FailedAttempts    int                   `json:"failedAttempts"`
	LockoutUntil      time.Time             `json:"lockoutUntil"`
}

// LoadOrCreate loads existing config or creates a new one
func LoadOrCreate(baseDir string) (*Config, error) {
	cfgFile := filepath.Join(baseDir, "config.json")
	cfg := &Config{
		filePath:          cfgFile,
		AuthorizedDevices: make(map[string]DeviceInfo),
	}

	data, err := os.ReadFile(cfgFile)
	if err == nil {
		if err := json.Unmarshal(data, cfg); err == nil {
			if cfg.AuthorizedDevices == nil {
				cfg.AuthorizedDevices = make(map[string]DeviceInfo)
			}
			// Verify cryptographic keypair validity
			validKeys := false
			if cfg.LaptopPrivKey != "" {
				derivedPub, err := nostr.GetPublicKey(cfg.LaptopPrivKey)
				if err == nil && derivedPub != "" {
					if cfg.LaptopPubKey != derivedPub {
						cfg.LaptopPubKey = derivedPub
						_ = cfg.Save()
					}
					validKeys = true
				}
			}
			if validKeys {
				return cfg, nil
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

// RevokeAllDevices unpairs all registered devices
func (c *Config) RevokeAllDevices() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.AuthorizedDevices = make(map[string]DeviceInfo)
	return c.saveLocked()
}

// UpdateDeviceLastSeen refreshes the last active timestamp for an authorized device
func (c *Config) UpdateDeviceLastSeen(pubKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if dev, ok := c.AuthorizedDevices[pubKey]; ok {
		dev.LastSeen = time.Now()
		c.AuthorizedDevices[pubKey] = dev
		_ = c.saveLocked()
	}
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

// GetAuthorizedDevices returns a snapshot copy of authorized devices
func (c *Config) GetAuthorizedDevices() map[string]DeviceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res := make(map[string]DeviceInfo, len(c.AuthorizedDevices))
	for k, v := range c.AuthorizedDevices {
		res[k] = v
	}
	return res
}

// Save persists config to file
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0600)
}
