package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func (c *Config) validate() error {
	c.Log.Level = strings.ToLower(c.Log.Level)
	c.Log.Format = strings.ToLower(c.Log.Format)
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("invalid log.level: expected debug, info, warn, or error")
	}
	if c.Log.Format != "text" && c.Log.Format != "json" {
		return errors.New("invalid log.format: expected text or json")
	}
	if strings.TrimSpace(c.Log.File.Path) == "" || c.Log.File.MaxSizeMB < 1 || int64(c.Log.File.MaxSizeMB) > math.MaxInt64/(1024*1024) || c.Log.File.MaxBackups < 0 || int64(c.Log.File.MaxBackups) == math.MaxInt64 || (int64(c.Log.File.MaxBackups)+1) > math.MaxInt64/(int64(c.Log.File.MaxSizeMB)*1024*1024) {
		return errors.New("invalid log.file: expected a path, positive max_size_mb and nonnegative max_backups within total size range")
	}
	for key, value := range map[string]int{"server.port": c.Server.Port, "postgres.port": c.Postgres.Port, "redis.port": c.Redis.Port} {
		if value < 1 || value > 65535 {
			return fmt.Errorf("invalid %s: expected a port from 1 to 65535", key)
		}
	}
	switch c.Postgres.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return errors.New("invalid postgres.sslmode")
	}
	origin, err := url.Parse(c.Server.PublicOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" {
		return errors.New("invalid server.public_origin")
	}
	if c.Redis.DB < 0 || c.Redis.DB > 15 {
		return errors.New("invalid redis.db")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(c.Tasks.Queue) {
		return errors.New("invalid tasks.queue")
	}
	for key, value := range map[string]int{
		"worker.max_concurrency": c.Worker.MaxConcurrency, "tasks.concurrency": c.Tasks.Concurrency, "tasks.max_rounds": c.Tasks.MaxRounds,
		"ai.routing.failure_threshold": c.AI.Routing.FailureThreshold, "ai.routing.max_response_bytes": c.AI.Routing.MaxResponseBytes, "ai.routing.max_output_tokens": c.AI.Routing.MaxOutputTokens,
	} {
		if value <= 0 {
			return fmt.Errorf("invalid %s: expected a positive integer", key)
		}
	}
	for key, value := range map[string]time.Duration{
		"worker.lease": c.Worker.Lease, "worker.poll_interval": c.Worker.PollInterval,
		"ai.request_timeout": c.AI.RequestTimeout, "ai.round_timeout": c.AI.RoundTimeout, "ai.routing.cooldown": c.AI.Routing.Cooldown,
		"redis.dial_timeout": c.Redis.DialTimeout, "redis.read_timeout": c.Redis.ReadTimeout, "redis.write_timeout": c.Redis.WriteTimeout,
		"tasks.retry_initial": c.Tasks.RetryInitial, "tasks.retry_max": c.Tasks.RetryMax, "tasks.shutdown_timeout": c.Tasks.ShutdownTimeout,
		"tasks.outbox_poll_interval": c.Tasks.OutboxPollInterval, "tasks.cancellation_poll_interval": c.Tasks.CancellationPollInterval,
	} {
		if value <= 0 {
			return fmt.Errorf("invalid %s: expected a positive duration", key)
		}
	}
	if c.Tasks.ShutdownTimeout <= c.AI.RoundTimeout || c.Tasks.RetryMax < c.Tasks.RetryInitial {
		return errors.New("invalid task timeout or retry budget")
	}
	return validateProviders(c)
}
