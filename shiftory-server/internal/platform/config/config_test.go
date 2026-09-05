package config

import "testing"

func TestLoadUsesLocalDefaults(t *testing.T) {
	t.Setenv("SHIFTORY_HTTP_ADDR", "")
	t.Setenv("SHIFTORY_DATABASE_DSN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("unexpected HTTP address %q", cfg.HTTPAddr)
	}
	if cfg.DatabaseDSN == "" || cfg.UploadDir == "" {
		t.Fatalf("required defaults missing: %+v", cfg)
	}
}

func TestLoadRejectsInvalidPublicOrigin(t *testing.T) {
	t.Setenv("SHIFTORY_PUBLIC_ORIGIN", "not-a-url")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid origin to fail")
	}
}
