package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

func VerifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(got) != len(challenge) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
