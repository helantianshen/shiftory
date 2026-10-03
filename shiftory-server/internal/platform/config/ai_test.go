package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderArrayEnvironmentAndPriority(t *testing.T) {
	clearConfigEnvironment(t)
	sourcePath := filepath.Join("..", "..", "..", "..", "config", "ai.development.example.yaml")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	content := strings.Replace(string(source), "enabled: true", "enabled: false", 1)
	writeConfig(t, "ai.yaml", content)
	t.Setenv("SHIFTORY_AI_PROVIDERS_DEEPSEEK_ORDER", "5")
	cfg, err := Load("--config", "ai.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AI.Providers) != 3 || cfg.AI.Providers[0].ID != "deepseek" || cfg.AI.Providers[0].Order != 5 {
		t.Fatal("provider override not sorted")
	}
	t.Setenv("SHIFTORY_AI_PROVIDERS_DEEPSEEK_ORDER", "10")
	if _, err = Load("--config", "ai.yaml"); err == nil {
		t.Fatal("duplicate order accepted")
	}
	t.Setenv("SHIFTORY_AI_PROVIDERS_DEEPSEEK_ORDER", "5")
	writeConfig(t, "bad.yaml", strings.Replace(content, "model: doubao-seed-character-260628", "unknown_secret: confidential", 1))
	if _, err = Load("--config", "bad.yaml"); err == nil || strings.Contains(err.Error(), "confidential") {
		t.Fatal("unknown field or redaction failed")
	}
}
