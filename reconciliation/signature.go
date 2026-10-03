package reconciliation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func Sign(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func ValidSignature(payload []byte, signature string, secret []byte) bool {
	if len(secret) == 0 || signature == "" {
		return false
	}
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}
