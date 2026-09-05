package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr         string
	DatabaseDSN      string
	UploadDir        string
	WebDir           string
	PublicOrigin     string
	JWTIssuer        string
	JWTAudience      string
	JWTPrivateKey    string
	JWTPublicKey     string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	AIModel          string
	AIBaseURL        string
	AIAPIKey         string
	WorkerID         string
	WorkerLease      time.Duration
	WorkerPollPeriod time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:         envOr("SHIFTORY_HTTP_ADDR", ":8080"),
		DatabaseDSN:      envOr("SHIFTORY_DATABASE_DSN", "root:123456@tcp(127.0.0.1:3306)/shiftory?charset=utf8mb4&parseTime=true&loc=UTC"),
		UploadDir:        envOr("SHIFTORY_UPLOAD_DIR", "./uploads"),
		WebDir:           envOr("SHIFTORY_WEB_DIR", "../shiftory-web/dist"),
		PublicOrigin:     envOr("SHIFTORY_PUBLIC_ORIGIN", "http://localhost:5173"),
		JWTIssuer:        envOr("SHIFTORY_JWT_ISSUER", "shiftory-local"),
		JWTAudience:      envOr("SHIFTORY_JWT_AUDIENCE", "shiftory-web"),
		JWTPrivateKey:    envOr("SHIFTORY_JWT_PRIVATE_KEY_FILE", "./var/jwt-private.pem"),
		JWTPublicKey:     envOr("SHIFTORY_JWT_PUBLIC_KEY_FILE", "./var/jwt-public.pem"),
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  7 * 24 * time.Hour,
		AIModel:          strings.TrimSpace(os.Getenv("SHIFTORY_AI_MODEL")),
		AIBaseURL:        strings.TrimSpace(os.Getenv("SHIFTORY_AI_BASE_URL")),
		AIAPIKey:         strings.TrimSpace(os.Getenv("SHIFTORY_AI_API_KEY")),
		WorkerID:         envOr("SHIFTORY_WORKER_ID", "shiftory-worker-local"),
		WorkerLease:      2 * time.Minute,
		WorkerPollPeriod: 2 * time.Second,
	}
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" {
		return Config{}, fmt.Errorf("invalid SHIFTORY_PUBLIC_ORIGIN %q", cfg.PublicOrigin)
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
