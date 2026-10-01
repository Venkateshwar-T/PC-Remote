//go:build !windows

package config

// EncryptDPAPI is a pass-through fallback for non-Windows environments (such as testing on Linux/macOS).
func EncryptDPAPI(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// DecryptDPAPI is a pass-through fallback for non-Windows environments.
func DecryptDPAPI(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
