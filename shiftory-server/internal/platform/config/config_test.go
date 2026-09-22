package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// clearConfigEnvironment 清理测试涉及的进程配置变量，并由测试框架恢复原值
func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"SHIFTORY_ENV_FILE",
		"SHIFTORY_ENV",
		"SHIFTORY_LOG_LEVEL",
		"SHIFTORY_LOG_FORMAT",
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

// writeConfig 在测试目录写入 YAML 配置样本
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadProfiles 验证默认生产模式及显式开发、生产模式选择
func TestLoadProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, environment, level, format, addr string
		args                                   []string
	}{
		{"default", "production", "info", "json", ":8081", nil},
		{"production", "production", "info", "json", ":8081", []string{"--env", "production"}},
		{"development", "development", "debug", "text", ":9091", []string{"--env=development"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Chdir(t.TempDir())
			writeConfig(t, "config/production.yaml", "http_addr: ':8081'\n")
			writeConfig(t, "config/development.yaml", "http_addr: ':9091'\n")
			// 模式只能由启动参数选择，SHIFTORY_ENV 不得覆盖参数或默认值
			t.Setenv("SHIFTORY_ENV", "development")
			cfg, err := Load(tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Environment != tc.environment || cfg.LogLevel != tc.level || cfg.LogFormat != tc.format || cfg.HTTPAddr != tc.addr {
				t.Fatalf("unexpected profile: %+v", cfg)
			}
		})
	}
}

// TestLoadDefaultsAndIgnoresEnvFiles 验证内置默认值以及后端不读取 env 文件
func TestLoadDefaultsAndIgnoresEnvFiles(t *testing.T) {
	clearConfigEnvironment(t)
	t.Chdir(t.TempDir())
	for _, name := range []string{".env", ".env.local", ".env.production", ".env.development", "shiftory.env"} {
		writeConfig(t, name, "SHIFTORY_HTTP_ADDR=:9191\nSHIFTORY_LOG_LEVEL=debug\n")
	}
	t.Setenv("SHIFTORY_ENV_FILE", "shiftory.env")
	writeConfig(t, "config/development.yaml", "http_addr: ':9091'\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.HTTPAddr != ":8080" || cfg.LogLevel != "info" || cfg.LogFormat != "json" || cfg.AIEnabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DatabaseDSN == "" || cfg.UploadDir == "" {
		t.Fatal("required defaults missing")
	}
}

// TestLoadFindsNearestProfile 验证逐级发现首个模式配置文件的规则
func TestLoadFindsNearestProfile(t *testing.T) {
	for _, depth := range []int{0, 1, 2} {
		t.Run(string(rune('0'+depth)), func(t *testing.T) {
			clearConfigEnvironment(t)
			root := t.TempDir()
			writeConfig(t, filepath.Join(root, "config/development.yaml"), "http_addr: ':9292'\n")
			cwd := root
			for i := 0; i < depth; i++ {
				cwd = filepath.Join(cwd, "nested")
			}
			if err := os.MkdirAll(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			cfg, err := Load("--env", "development")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr != ":9292" {
				t.Fatalf("profile not found: %q", cfg.HTTPAddr)
			}
			writeConfig(t, "config/development.yaml", "http_addr: ':9393'\n")
			cfg, err = Load("--env", "development")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr != ":9393" {
				t.Fatalf("nearest profile not selected: %q", cfg.HTTPAddr)
			}
		})
	}
}

// TestLoadExplicitYAMLAndEnvironmentOverrides 验证显式 YAML 与非空进程环境变量的覆盖顺序
func TestLoadExplicitYAMLAndEnvironmentOverrides(t *testing.T) {
	clearConfigEnvironment(t)
	t.Chdir(t.TempDir())
	content := `log_level: warn
log_format: text
http_addr: ':9090'
database_dsn: 'user:password@tcp(localhost:3306)/test'
upload_dir: ./data/uploads
web_dir: ./web
public_origin: https://from-file.example
jwt_issuer: test-issuer
jwt_audience: test-audience
jwt_private_key_file: ./keys/private.pem
jwt_public_key_file: ./keys/public.pem
ai_model: vision-test
ai_base_url: https://ai.example/v1
ai_api_key: test-key
ai_enabled: true
ai_request_timeout: 45s
worker_id: test-worker
worker_poll_interval: 7s
worker_lease: 3m
worker_max_concurrency: 4
`
	writeConfig(t, "selected config.yml", content)
	writeConfig(t, "config/production.yaml", "http_addr: ':9191'\n")
	cfg, err := Load("--config", "selected config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.LogLevel != "warn" || cfg.LogFormat != "text" || cfg.HTTPAddr != ":9090" || cfg.DatabaseDSN != "user:password@tcp(localhost:3306)/test" || cfg.UploadDir != "./data/uploads" || cfg.WebDir != "./web" || cfg.PublicOrigin != "https://from-file.example" || cfg.JWTIssuer != "test-issuer" || cfg.JWTAudience != "test-audience" || cfg.JWTPrivateKey != "./keys/private.pem" || cfg.JWTPublicKey != "./keys/public.pem" || cfg.AIModel != "vision-test" || cfg.AIBaseURL != "https://ai.example/v1" || cfg.AIAPIKey != "test-key" || !cfg.AIEnabled || cfg.AIRequestTimeout.String() != "45s" || cfg.WorkerID != "test-worker" || cfg.WorkerPollPeriod.String() != "7s" || cfg.WorkerLease.String() != "3m0s" || cfg.WorkerMaxConcurrency != 4 {
		t.Fatalf("YAML values were not loaded: %+v", cfg)
	}
	if os.Getenv("SHIFTORY_HTTP_ADDR") != "" {
		t.Fatal("loading mutated process environment")
	}
	values, err := parseYAMLFile("selected config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	writeConfig(t, "empty.yaml", "{}\n")
	fromEnv, err := Load("--config", "empty.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, fromEnv) {
		t.Fatal("environment and YAML settings differ")
	}
	t.Setenv("SHIFTORY_HTTP_ADDR", ":7070")
	t.Setenv("SHIFTORY_AI_ENABLED", "false")
	t.Setenv("SHIFTORY_WORKER_MAX_CONCURRENCY", "6")
	t.Setenv("SHIFTORY_WORKER_LEASE", "4m")
	cfg, err = Load("--config", "selected config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":7070" || cfg.AIEnabled || cfg.WorkerMaxConcurrency != 6 || cfg.WorkerLease.String() != "4m0s" {
		t.Fatalf("environment did not override YAML: %+v", cfg)
	}
	t.Setenv("SHIFTORY_HTTP_ADDR", "  ")
	cfg, err = Load("--config", "selected config.yml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatal("empty environment should fall back to YAML")
	}
}

// TestLoadRejectsInvalidArguments 验证非法模式、未知参数和额外位置参数被拒绝
func TestLoadRejectsInvalidArguments(t *testing.T) {
	clearConfigEnvironment(t)
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"--env", "staging"}, {"--env="}, {"--env"}, {"--unknown"}, {"development"},
		{"--config", "missing.yaml"}, {"--config", "legacy.env"},
	} {
		if _, err := Load(args...); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
	if _, err := Load("--help"); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
}

// TestLoadRejectsInvalidYAML 验证非法 YAML、未知字段和不支持的文件形式被拒绝
func TestLoadRejectsInvalidYAML(t *testing.T) {
	for _, content := range []string{
		"http_addr: [", "http_addr: ':8080'\nhttp_addr: ':9090'\n", "unknown: value\n",
		"http_addr: [a, b]\n", "http_addr: {nested: value}\n", "- value\n", "null\n", "",
		"http_addr: ':8080'\n---\nhttp_addr: ':9090'\n", "env: development\n",
	} {
		t.Run(content, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Chdir(t.TempDir())
			writeConfig(t, "invalid.yaml", content)
			if _, err := Load("--config", "invalid.yaml"); err == nil {
				t.Fatal("invalid YAML accepted")
			}
			writeConfig(t, "config/production.yaml", content)
			if _, err := Load(); err == nil {
				t.Fatal("invalid automatically discovered YAML accepted")
			}
		})
	}
}

// TestLoadRejectsInvalidSettings 验证无效日志、时间、并发与来源设置被拒绝
func TestLoadRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"log_level", "verbose"}, {"log_format", "xml"}, {"public_origin", "not-a-url"},
		{"worker_max_concurrency", "0"}, {"worker_max_concurrency", "-1"}, {"worker_max_concurrency", "1.5"},
		{"worker_lease", "0s"}, {"worker_poll_interval", "-1s"}, {"ai_request_timeout", "oops"}, {"ai_enabled", "maybe"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Chdir(t.TempDir())
			writeConfig(t, "invalid.yaml", tc.key+": '"+tc.value+"'\n")
			if _, err := Load("--config", "invalid.yaml"); err == nil {
				t.Fatal("invalid YAML setting accepted")
			}
			t.Setenv("SHIFTORY_"+strings.ToUpper(tc.key), tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid environment setting accepted")
			}
		})
	}
}

// TestDistributedYAMLExamples 验证随仓库分发的开发和生产 YAML 样例可以加载
func TestDistributedYAMLExamples(t *testing.T) {
	clearConfigEnvironment(t)
	for _, environment := range []string{"development", "production"} {
		path := filepath.Join("..", "..", "..", "..", "config", environment+".example.yaml")
		values, err := parseYAMLFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != len(yamlKeys) {
			t.Fatalf("%s does not document every supported YAML setting", path)
		}
		cfg, err := Load("--env", environment, "--config", path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Environment != environment || cfg.AIEnabled || cfg.AIAPIKey != "" {
			t.Fatalf("%s has unexpected profile or AI defaults", path)
		}
	}
}
