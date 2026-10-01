package ipc

import "encoding/json"

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

// MigrateKeyParams carries a decrypted private key across local IPC for service DPAPI re-encryption
type MigrateKeyParams struct {
	PrivateKey string `json:"privateKey"`
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
