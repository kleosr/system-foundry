package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type modelConfig struct {
	BaseURL      string
	APIKey       string
	Primary      string
	Cheap        string
	Embed        string
	PrimaryPrice int64
	CheapPrice   int64
}

type langfuseConfig struct {
	Host   string
	Public string
	Secret string
}

type runtimeConfig struct {
	Mode          string
	Addr          string
	Postgres      string
	NATS          string
	Redis         string
	Origin        string
	Temporal      string
	DemoPassword  string
	WebhookSecret string
	OpsTenant     string
	ChaosEnabled  bool
	RatePerMinute int
	Model         modelConfig
	Langfuse      langfuseConfig
	Identity      identityConfig
}

type identityConfig struct {
	SigningKeyFile       string
	OperatorEmail        string
	OperatorPasswordFile string
	ClientRedirects      []string
}

func loadConfig(getenv func(string) string) (runtimeConfig, error) {
	cfg := runtimeConfig{
		Mode:          getenv("FOUNDRY_MODE"),
		Addr:          getenv("FOUNDRY_HTTP_ADDR"),
		Postgres:      getenv("FOUNDRY_POSTGRES_DSN"),
		NATS:          getenv("FOUNDRY_NATS_URL"),
		Redis:         getenv("FOUNDRY_REDIS_ADDR"),
		Origin:        getenv("FOUNDRY_ORIGIN"),
		Temporal:      getenv("FOUNDRY_TEMPORAL_ADDR"),
		DemoPassword:  getenv("FOUNDRY_DEMO_PASSWORD"),
		WebhookSecret: getenv("FOUNDRY_WEBHOOK_SECRET"),
		OpsTenant:     getenv("FOUNDRY_OPS_TENANT"),
		ChaosEnabled:  getenv("FOUNDRY_CHAOS_ENABLED") == "true",
		Model: modelConfig{
			BaseURL: getenv("FOUNDRY_MODEL_BASE_URL"),
			APIKey:  getenv("FOUNDRY_MODEL_API_KEY"),
			Primary: getenv("FOUNDRY_PRIMARY_MODEL"),
			Cheap:   getenv("FOUNDRY_CHEAP_MODEL"),
			Embed:   getenv("FOUNDRY_EMBED_MODEL"),
		},
		Langfuse: langfuseConfig{
			Host:   getenv("FOUNDRY_LANGFUSE_HOST"),
			Public: getenv("FOUNDRY_LANGFUSE_PUBLIC_KEY"),
			Secret: getenv("FOUNDRY_LANGFUSE_SECRET_KEY"),
		},
		Identity: identityConfig{
			SigningKeyFile:       getenv("FOUNDRY_SIGNING_KEY_FILE"),
			OperatorEmail:        getenv("FOUNDRY_BOOTSTRAP_OPERATOR_EMAIL"),
			OperatorPasswordFile: getenv("FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE"),
			ClientRedirects:      splitList(getenv("FOUNDRY_OAUTH_REDIRECTS")),
		},
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	if cfg.Origin == "" {
		cfg.Origin = "http://127.0.0.1:8080"
	}
	if cfg.OpsTenant == "" {
		cfg.OpsTenant = "ops"
	}
	if cfg.Langfuse.Host == "" {
		cfg.Langfuse.Host = "https://cloud.langfuse.com"
	}
	var err error
	if cfg.RatePerMinute, err = positiveInt(getenv("FOUNDRY_RATE_LIMIT_PER_MINUTE"), 600); err != nil {
		return runtimeConfig{}, fmt.Errorf("FOUNDRY_RATE_LIMIT_PER_MINUTE: %w", err)
	}
	if cfg.Model, err = completeModelConfig(cfg.Model, getenv); err != nil {
		return runtimeConfig{}, err
	}
	switch cfg.Mode {
	case "local":
		if cfg.DemoPassword != "" && len(cfg.DemoPassword) < 12 {
			return runtimeConfig{}, errors.New("FOUNDRY_DEMO_PASSWORD must have at least 12 characters")
		}
		return cfg, nil
	case "shared":
		if cfg.Postgres == "" || cfg.NATS == "" || cfg.Redis == "" {
			return runtimeConfig{}, errors.New("shared mode requires FOUNDRY_POSTGRES_DSN, FOUNDRY_NATS_URL, and FOUNDRY_REDIS_ADDR")
		}
		if cfg.DemoPassword != "" {
			return runtimeConfig{}, errors.New("FOUNDRY_DEMO_PASSWORD seeds demo accounts and is refused in shared mode")
		}
		if cfg.Identity.SigningKeyFile == "" {
			return runtimeConfig{}, errors.New("shared mode requires FOUNDRY_SIGNING_KEY_FILE so every replica signs with one key")
		}
		if (cfg.Identity.OperatorEmail == "") != (cfg.Identity.OperatorPasswordFile == "") {
			return runtimeConfig{}, errors.New("set both FOUNDRY_BOOTSTRAP_OPERATOR_EMAIL and FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE, or neither")
		}
		return cfg, nil
	default:
		return runtimeConfig{}, fmt.Errorf("FOUNDRY_MODE must be local or shared")
	}
}

func configFromEnv() (runtimeConfig, error) {
	return loadConfig(os.Getenv)
}

func completeModelConfig(model modelConfig, getenv func(string) string) (modelConfig, error) {
	if model.BaseURL == "" {
		model.Primary, model.Cheap = "primary", "cheap"
		model.PrimaryPrice, model.CheapPrice = 30_000, 1_000
		return model, nil
	}
	if model.Primary == "" || model.Cheap == "" {
		return modelConfig{}, errors.New("FOUNDRY_MODEL_BASE_URL requires FOUNDRY_PRIMARY_MODEL and FOUNDRY_CHEAP_MODEL")
	}
	var err error
	if model.PrimaryPrice, err = positivePrice(getenv("FOUNDRY_PRIMARY_PRICE_MICROS_PER_1K")); err != nil {
		return modelConfig{}, fmt.Errorf("FOUNDRY_PRIMARY_PRICE_MICROS_PER_1K: %w", err)
	}
	if model.CheapPrice, err = positivePrice(getenv("FOUNDRY_CHEAP_PRICE_MICROS_PER_1K")); err != nil {
		return modelConfig{}, fmt.Errorf("FOUNDRY_CHEAP_PRICE_MICROS_PER_1K: %w", err)
	}
	return model, nil
}

func splitList(raw string) []string {
	var items []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func positiveInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("must be a positive integer")
	}
	return value, nil
}

func positivePrice(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("must be a positive integer of micro-USD per 1000 tokens")
	}
	return value, nil
}
