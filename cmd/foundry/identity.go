package main

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

func buildProvider(cfg runtimeConfig, stores *backends, logger *slog.Logger) (*auth.Provider, error) {
	if cfg.Mode == "local" {
		provider, err := auth.NewProvider(cfg.Origin, time.Now, auth.Params{})
		if err != nil {
			return nil, err
		}
		return provider, seedDemo(provider, cfg.DemoPassword, cfg.OpsTenant)
	}
	keyPEM, err := os.ReadFile(cfg.Identity.SigningKeyFile)
	if err != nil {
		return nil, errors.New("FOUNDRY_SIGNING_KEY_FILE is not readable")
	}
	provider, err := auth.NewSharedProvider(cfg.Origin, auth.Params{}, keyPEM, auth.NewPostgresCredentials(stores.pool, logger))
	if err != nil {
		return nil, err
	}
	redirects := cfg.Identity.ClientRedirects
	if len(redirects) == 0 {
		redirects = []string{demoRedirect}
	}
	if err = provider.RegisterClient(demoClientID, apiAudience, redirects); err != nil {
		return nil, err
	}
	if cfg.Identity.OperatorEmail == "" {
		return provider, nil
	}
	password, err := os.ReadFile(cfg.Identity.OperatorPasswordFile)
	if err != nil {
		return nil, errors.New("FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE is not readable")
	}
	trimmed := strings.TrimSpace(string(password))
	if len(trimmed) < 12 {
		return nil, errors.New("bootstrap operator password must have at least 12 characters")
	}
	return provider, provider.EnsureUser(cfg.Identity.OperatorEmail, trimmed, cfg.OpsTenant, platform.RoleOwner)
}
