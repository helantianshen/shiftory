package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"SHIFTORY_ENV_FILE",
		"SHIFTORY_HTTP_ADDR",
		"SHIFTORY_DATABASE_DSN",
		"SHIFTORY_UPLOAD_DIR",
		"SHIFTORY_WEB_DIR",
		"SHIFTORY_PUBLIC_ORIGIN",
		"SHIFTORY_JWT_ISSUER",
		"SHIFTORY_JWT_AUDIENCE",
		"SHIFTORY_JWT_PRIVATE_KEY_FILE",
		"SHIFTORY_JWT_PUBLIC_KEY_FILE",
		"SHIFTORY_AI_MODEL",
		"SHIFTORY_AI_BASE_URL",
		"SHIFTORY_AI_API_KEY",
		"SHIFTORY_AI_REQUEST_TIMEOUT",
		"SHIFTORY_AI_ENABLED",
		"SHIFTORY_WORKER_ID",
		"SHIFTORY_WORKER_POLL_INTERVAL",
		"SHIFTORY_WORKER_LEASE",
		"SHIFTORY_WORKER_MAX_CONCURRENCY",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadUsesLocalDefaults(t *testing.T) {
	clearConfigEnvironment(t)
	t.Chdir(t.TempDir())

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

func TestLoadUsesExplicitEnvFileForMissingEnvironment(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("SHIFTORY_HTTP_ADDR=127.0.0.1:9090\nSHIFTORY_AI_MODEL='vision-test'\nSHIFTORY_AI_API_KEY=test-key\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" || cfg.AIModel != "vision-test" || cfg.AIAPIKey != "test-key" {
		t.Fatalf("env file values were not loaded: %+v", cfg)
	}
}

func TestLoadEnvironmentOverridesExplicitEnvFile(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("SHIFTORY_HTTP_ADDR=127.0.0.1:9090\nSHIFTORY_PUBLIC_ORIGIN=https://from-file.example\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)
	t.Setenv("SHIFTORY_HTTP_ADDR", ":7070")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != ":7070" {
		t.Fatalf("system environment was overwritten by env file: %q", cfg.HTTPAddr)
	}
}

func TestLoadAutomaticallyFindsDevelopmentEnvNearWorkingDirectory(t *testing.T) {
	clearConfigEnvironment(t)
	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)
	if err := os.WriteFile(filepath.Join(workingDirectory, ".env.development"), []byte("SHIFTORY_HTTP_ADDR=127.0.0.1:9191\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9191" {
		t.Fatalf("automatic env discovery returned %q", cfg.HTTPAddr)
	}
}

func TestLoadAutomaticallyFindsDevelopmentEnvInParentDirectory(t *testing.T) {
	clearConfigEnvironment(t)
	parentDirectory := t.TempDir()
	workingDirectory := filepath.Join(parentDirectory, "shiftory-server")
	if err := os.Mkdir(workingDirectory, 0o700); err != nil {
		t.Fatalf("create working directory: %v", err)
	}
	t.Chdir(workingDirectory)
	if err := os.WriteFile(filepath.Join(parentDirectory, ".env.development"), []byte("SHIFTORY_HTTP_ADDR=127.0.0.1:9292\n"), 0o600); err != nil {
		t.Fatalf("write parent env file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9292" {
		t.Fatalf("parent env discovery returned %q", cfg.HTTPAddr)
	}
}

func TestLoadRejectsMissingExplicitEnvFile(t *testing.T) {
	clearConfigEnvironment(t)
	t.Chdir(t.TempDir())
	t.Setenv("SHIFTORY_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

	if _, err := Load(); err == nil {
		t.Fatal("missing explicitly configured env file should fail config loading")
	}
}

func TestLoadRejectsMalformedExplicitEnvFile(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("not-an-environment-entry\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)

	if _, err := Load(); err == nil {
		t.Fatal("malformed explicitly configured env file should fail config loading")
	}
}

func TestLoadParsesWorkerSettingsFromEnvFile(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("SHIFTORY_WORKER_POLL_INTERVAL=7s\nSHIFTORY_WORKER_LEASE=3m\nSHIFTORY_WORKER_MAX_CONCURRENCY=4\nSHIFTORY_AI_REQUEST_TIMEOUT=45s\nSHIFTORY_AI_ENABLED=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.WorkerPollPeriod.String() != "7s" || cfg.WorkerLease.String() != "3m0s" || cfg.WorkerMaxConcurrency != 4 || cfg.AIRequestTimeout.String() != "45s" || !cfg.AIEnabled {
		t.Fatalf("worker settings were not parsed: %+v", cfg)
	}
}

func TestLoadRejectsInvalidWorkerConcurrency(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("SHIFTORY_WORKER_MAX_CONCURRENCY=0\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)

	if _, err := Load(); err == nil {
		t.Fatal("invalid worker concurrency should fail config loading")
	}
}

func TestLoadRejectsInvalidAIEnabledValue(t *testing.T) {
	clearConfigEnvironment(t)
	envFile := filepath.Join(t.TempDir(), "shiftory.env")
	if err := os.WriteFile(envFile, []byte("SHIFTORY_AI_ENABLED=maybe\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("SHIFTORY_ENV_FILE", envFile)

	if _, err := Load(); err == nil {
		t.Fatal("invalid AI enabled value should fail config loading")
	}
}

func TestLoadRejectsInvalidPublicOrigin(t *testing.T) {
	t.Setenv("SHIFTORY_PUBLIC_ORIGIN", "not-a-url")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid origin to fail")
	}
}
