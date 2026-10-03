package main

import "testing"

func TestSharedModeRejectsMissingStores(t *testing.T) {
	_, err := loadConfig(func(key string) string {
		if key == "FOUNDRY_MODE" {
			return "shared"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLocalModeStartsWithoutStores(t *testing.T) {
	cfg, err := loadConfig(func(key string) string {
		if key == "FOUNDRY_MODE" {
			return "local"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Fatalf("addr = %s", cfg.Addr)
	}
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestSharedModeRefusesDemoAccounts(t *testing.T) {
	_, err := loadConfig(envOf(map[string]string{
		"FOUNDRY_MODE": "shared", "FOUNDRY_POSTGRES_DSN": "postgres://x", "FOUNDRY_NATS_URL": "nats://x",
		"FOUNDRY_REDIS_ADDR": "x:6379", "FOUNDRY_DEMO_PASSWORD": "a-long-demo-password",
	}))
	if err == nil {
		t.Fatal("shared mode accepted demo accounts")
	}
}

func TestSharedModeNeedsOneSigningKey(t *testing.T) {
	env := map[string]string{
		"FOUNDRY_MODE": "shared", "FOUNDRY_POSTGRES_DSN": "postgres://x", "FOUNDRY_NATS_URL": "nats://x", "FOUNDRY_REDIS_ADDR": "x:6379",
	}
	if _, err := loadConfig(envOf(env)); err == nil {
		t.Fatal("shared mode started with per-process keys")
	}
	env["FOUNDRY_SIGNING_KEY_FILE"] = "/run/secrets/signing.pem"
	env["FOUNDRY_BOOTSTRAP_OPERATOR_EMAIL"] = "op@example.com"
	if _, err := loadConfig(envOf(env)); err == nil {
		t.Fatal("operator email accepted without a password file")
	}
	env["FOUNDRY_BOOTSTRAP_OPERATOR_PASSWORD_FILE"] = "/run/secrets/operator"
	env["FOUNDRY_OAUTH_REDIRECTS"] = "https://a.example/cb, https://b.example/cb"
	cfg, err := loadConfig(envOf(env))
	if err != nil || len(cfg.Identity.ClientRedirects) != 2 {
		t.Fatalf("cfg=%+v err=%v", cfg.Identity, err)
	}
}

func TestHostedModelNeedsNamesAndPrices(t *testing.T) {
	base := map[string]string{"FOUNDRY_MODE": "local", "FOUNDRY_MODEL_BASE_URL": "https://api.example/v1"}
	if _, err := loadConfig(envOf(base)); err == nil {
		t.Fatal("missing model names accepted")
	}
	base["FOUNDRY_PRIMARY_MODEL"], base["FOUNDRY_CHEAP_MODEL"] = "big", "small"
	if _, err := loadConfig(envOf(base)); err == nil {
		t.Fatal("missing prices accepted")
	}
	base["FOUNDRY_PRIMARY_PRICE_MICROS_PER_1K"], base["FOUNDRY_CHEAP_PRICE_MICROS_PER_1K"] = "5000", "150"
	cfg, err := loadConfig(envOf(base))
	if err != nil || cfg.Model.PrimaryPrice != 5000 || cfg.RatePerMinute != 600 {
		t.Fatalf("cfg=%+v err=%v", cfg.Model, err)
	}
}

func TestMissingModeRefusesStart(t *testing.T) {
	_, err := loadConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error")
	}
}
