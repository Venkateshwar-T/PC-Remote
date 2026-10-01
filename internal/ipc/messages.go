package ipc

import (
	"encoding/json"
	"time"
)

const (
	PipeName        = `\\.\pipe\PCRemote`
	MaxMessageSize  = 64 * 1024 // 64KB strict boundary to prevent buffer exhaustion
	ProtocolVersion = "2.0"
)

// Standard IPC method names (Strict Allowlist)
const (
	MethodGetStatus          = "GetStatus"
	MethodGetPairingInfo     = "GetPairingInfo"
	MethodRotatePairingToken = "RotatePairingToken"
	MethodGetPinStatus       = "GetPinStatus"
	MethodSetPin             = "SetPin"
	MethodMigrateLegacyKey   = "MigrateLegacyKey"
	MethodStopService        = "StopService"
)

// Request defines the JSON-RPC request structure
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response defines the JSON-RPC response structure
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// StatusResult returns general daemon operational metrics
type StatusResult struct {
	Running     bool   `json:"running"`
	Version     string `json:"version"`
	DeviceName  string `json:"deviceName"`
	PairedCount int    `json:"pairedCount"`
	Port        int    `json:"port"`
	Uptime      string `json:"uptime"`
}

// PairingInfoResult holds the active pairing token and dynamic pairing URL
type PairingInfoResult struct {
	URL          string `json:"url"`
	Token        string `json:"token"`
	ExpiryUnix   int64  `json:"expiryUnix"`
	LaptopPubKey string `json:"laptopPubKey"`
}

// PinStatusResult indicates whether a master PIN has been set
type PinStatusResult struct {
	IsSet bool `json:"isSet"`
}

// SetPinParams contains parameters for setting or modifying the Master PIN
type SetPinParams struct {
	OldPin string `json:"oldPin"`
	NewPin string `json:"newPin"`
}

// SetPinResult reports PIN change status
type SetPinResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// DeviceInfo represents an authorized device metadata
type DeviceInfo struct {
	Name     string    `json:"name"`
	AddedAt  time.Time `json:"addedAt"`
	LastSeen time.Time `json:"lastSeen"`
}

// MigrateKeyParams carries a decrypted private key and compatible legacy configuration across local IPC for service DPAPI re-encryption and adoption
type MigrateKeyParams struct {
	PrivateKey        string                `json:"privateKey"`
	PinHash           string                `json:"pinHash,omitempty"`
	PinSalt           string                `json:"pinSalt,omitempty"`
	DeviceName        string                `json:"deviceName,omitempty"`
	AuthorizedDevices map[string]DeviceInfo `json:"authorizedDevices,omitempty"`
}

// MigrateKeyResult reports legacy migration outcome
type MigrateKeyResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// StopServiceResult reports service stop status
type StopServiceResult struct {
	Success bool `json:"success"`
}
