package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactingHandlerMasksMessageAttrsGroupsAndErrors(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewJSONHandler(&out, nil))).With("caller", "ada@example.com")
	logger.Info("login for bob@example.com",
		"auth", "Bearer abc.def.ghi",
		slog.Group("req", "ssn", "123-45-6789"),
		"err", errors.New("card 4111 1111 1111 1111 declined"),
		"status", 401,
	)
	text := out.String()
	for _, leaked := range []string{"ada@example.com", "bob@example.com", "abc.def.ghi", "123-45-6789", "4111 1111"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("leaked %q in %s", leaked, text)
		}
	}
	if !strings.Contains(text, `"status":401`) {
		t.Fatalf("non-string attr changed: %s", text)
	}
}
