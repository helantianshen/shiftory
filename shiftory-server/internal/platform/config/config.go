package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr             string
	DatabaseDSN          string
	UploadDir            string
	WebDir               string
	PublicOrigin         string
	JWTIssuer            string
	JWTAudience          string
	JWTPrivateKey        string
	JWTPublicKey         string
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	AIModel              string
	AIBaseURL            string
	AIAPIKey             string
	AIEnabled            bool
	WorkerID             string
	WorkerLease          time.Duration
	WorkerPollPeriod     time.Duration
	WorkerMaxConcurrency int
	AIRequestTimeout     time.Duration
}

func Load() (Config, error) {
	fileValues, err := loadEnvironmentFile()
	if err != nil {
		return Config{}, err
	}
	workerLease, err := durationOr("SHIFTORY_WORKER_LEASE", fileValues, 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	workerPollPeriod, err := durationOr("SHIFTORY_WORKER_POLL_INTERVAL", fileValues, 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerMaxConcurrency, err := positiveIntOr("SHIFTORY_WORKER_MAX_CONCURRENCY", fileValues, 2)
	if err != nil {
		return Config{}, err
	}
	aiRequestTimeout, err := durationOr("SHIFTORY_AI_REQUEST_TIMEOUT", fileValues, 90*time.Second)
	if err != nil {
		return Config{}, err
	}
	aiModel := envOr("SHIFTORY_AI_MODEL", fileValues, "")
	aiAPIKey := envOr("SHIFTORY_AI_API_KEY", fileValues, "")
	aiEnabled, err := boolOr("SHIFTORY_AI_ENABLED", fileValues, false)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPAddr:             envOr("SHIFTORY_HTTP_ADDR", fileValues, ":8080"),
		DatabaseDSN:          envOr("SHIFTORY_DATABASE_DSN", fileValues, "root:123456@tcp(127.0.0.1:3306)/shiftory?charset=utf8mb4&parseTime=true&loc=UTC"),
		UploadDir:            envOr("SHIFTORY_UPLOAD_DIR", fileValues, "./uploads"),
		WebDir:               envOr("SHIFTORY_WEB_DIR", fileValues, "../shiftory-web/dist"),
		PublicOrigin:         envOr("SHIFTORY_PUBLIC_ORIGIN", fileValues, "http://localhost:5173"),
		JWTIssuer:            envOr("SHIFTORY_JWT_ISSUER", fileValues, "shiftory-local"),
		JWTAudience:          envOr("SHIFTORY_JWT_AUDIENCE", fileValues, "shiftory-web"),
		JWTPrivateKey:        envOr("SHIFTORY_JWT_PRIVATE_KEY_FILE", fileValues, "./var/jwt-private.pem"),
		JWTPublicKey:         envOr("SHIFTORY_JWT_PUBLIC_KEY_FILE", fileValues, "./var/jwt-public.pem"),
		AccessTokenTTL:       15 * time.Minute,
		RefreshTokenTTL:      7 * 24 * time.Hour,
		AIModel:              aiModel,
		AIBaseURL:            envOr("SHIFTORY_AI_BASE_URL", fileValues, ""),
		AIAPIKey:             aiAPIKey,
		AIEnabled:            aiEnabled,
		WorkerID:             envOr("SHIFTORY_WORKER_ID", fileValues, "shiftory-worker-local"),
		WorkerLease:          workerLease,
		WorkerPollPeriod:     workerPollPeriod,
		WorkerMaxConcurrency: workerMaxConcurrency,
		AIRequestTimeout:     aiRequestTimeout,
	}
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" {
		return Config{}, fmt.Errorf("invalid SHIFTORY_PUBLIC_ORIGIN %q", cfg.PublicOrigin)
	}
	return cfg, nil
}

func envOr(name string, fileValues map[string]string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	if value := strings.TrimSpace(fileValues[name]); value != "" {
		return value
	}
	return fallback
}

func durationOr(name string, fileValues map[string]string, fallback time.Duration) (time.Duration, error) {
	value := envOr(name, fileValues, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: expected a positive duration", name, value)
	}
	return parsed, nil
}

func positiveIntOr(name string, fileValues map[string]string, fallback int) (int, error) {
	value := envOr(name, fileValues, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: expected a positive integer", name, value)
	}
	return parsed, nil
}

func boolOr(name string, fileValues map[string]string, fallback bool) (bool, error) {
	value := envOr(name, fileValues, "")
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: expected true or false", name, value)
	}
	return parsed, nil
}

func loadEnvironmentFile() (map[string]string, error) {
	explicit := strings.TrimSpace(os.Getenv("SHIFTORY_ENV_FILE"))
	if explicit != "" {
		return parseEnvironmentFile(explicit)
	}

	candidates := []string{filepath.Join(string(filepath.Separator), "etc", "shiftory", "shiftory.env")}
	if workingDirectory, err := os.Getwd(); err == nil {
		directories := []string{workingDirectory}
		for len(directories) < 3 {
			parent := filepath.Dir(directories[len(directories)-1])
			if parent == directories[len(directories)-1] {
				break
			}
			directories = append(directories, parent)
		}
		for _, directory := range directories {
			for _, name := range []string{".env.development", ".env.local", ".env", ".env.production"} {
				candidates = append(candidates, filepath.Join(directory, name))
			}
		}
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return parseEnvironmentFile(candidate)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect environment file %s: %w", candidate, err)
		}
	}
	return map[string]string{}, nil
}

func parseEnvironmentFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read environment file %s: %w", path, err)
	}
	values := make(map[string]string)
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		separator := strings.IndexByte(line, '=')
		if separator <= 0 {
			return nil, fmt.Errorf("invalid environment entry in %s at line %d", path, lineNumber+1)
		}
		name := strings.TrimSpace(line[:separator])
		if !isEnvironmentName(name) {
			return nil, fmt.Errorf("invalid environment variable name %q in %s at line %d", name, path, lineNumber+1)
		}
		value := strings.TrimSpace(line[separator+1:])
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
				value = value[1 : len(value)-1]
			}
		}
		values[name] = value
	}
	return values, nil
}

func isEnvironmentName(name string) bool {
	if name == "" || (name[0] < 'A' || name[0] > 'Z') && (name[0] < 'a' || name[0] > 'z') && name[0] != '_' {
		return false
	}
	for _, character := range name[1:] {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}
