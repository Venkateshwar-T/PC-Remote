//go:build windows

package config

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// EncryptDPAPI encrypts raw secret bytes using Windows Data Protection API (DPAPI).
// The ciphertext can only be decrypted by the same Windows user session.
func EncryptDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, 0, &outBlob)
	if err != nil {
		return nil, fmt.Errorf("windows CryptProtectData failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	outBytes := make([]byte, outBlob.Size)
	copy(outBytes, unsafe.Slice(outBlob.Data, outBlob.Size))
	return outBytes, nil
}

// DecryptDPAPI decrypts a DPAPI ciphertext blob using Windows Data Protection API.
func DecryptDPAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, 0, &outBlob)
	if err != nil {
		return nil, fmt.Errorf("windows CryptUnprotectData failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	outBytes := make([]byte, outBlob.Size)
	copy(outBytes, unsafe.Slice(outBlob.Data, outBlob.Size))
	return outBytes, nil
}
