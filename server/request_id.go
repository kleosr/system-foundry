package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func incomingID(r *http.Request) string {
	headerID := r.Header.Get("X-Request-ID")
	if requestIDPattern.MatchString(headerID) {
		return headerID
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "generated-request-id"
	}
	return hex.EncodeToString(buf)
}
