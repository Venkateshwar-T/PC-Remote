package qr

import (
	"encoding/base64"
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// GenerateBase64PNG creates a 100% offline, local QR code PNG data URI
func GenerateBase64PNG(data string, size int) (string, error) {
	pngBytes, err := qrcode.Encode(data, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	return fmt.Sprintf("data:image/png;base64,%s", encoded), nil
}

// GenerateTerminalANSI generates a scannable QR code directly in the console
func GenerateTerminalANSI(data string) string {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return ""
	}
	return qr.ToSmallString(false)
}
