package auth

import (
	"encoding/json"
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// PairPayload is the JSON payload encoded in the QR code.
type PairPayload struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// GenerateQRTerminal generates a QR code as a string for terminal display.
// The QR encodes a JSON payload with host, port, and one-time pairing token.
func GenerateQRTerminal(host string, port int, token string) (string, error) {
	payload := PairPayload{Host: host, Port: port, Token: token}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal pair payload: %w", err)
	}

	qr, err := qrcode.New(string(data), qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("generate qr: %w", err)
	}

	return qr.ToSmallString(false), nil
}

// GenerateQRPNG generates a QR code as PNG bytes for desktop display.
func GenerateQRPNG(host string, port int, token string, size int) ([]byte, error) {
	payload := PairPayload{Host: host, Port: port, Token: token}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal pair payload: %w", err)
	}

	return qrcode.Encode(string(data), qrcode.Medium, size)
}
