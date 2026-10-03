package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Provider struct {
	ID             string `json:"id"`
	Supplier       string `json:"supplier"`
	Kind           string `json:"kind"`
	Enabled        bool   `json:"enabled"`
	Order          int    `json:"order"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	ImageEnabled   bool   `json:"image_enabled"`
	TextEnabled    bool   `json:"text_enabled"`
	ResponseFormat string `json:"response_format"`
	Thinking       string `json:"thinking"`
	RequestTimeout string `json:"request_timeout"`
	MaxConcurrency int    `json:"max_concurrency"`
}
type AIConfig struct {
	Providers        []Provider
	RoundTimeout     time.Duration
	FailureThreshold int
	Cooldown         time.Duration
	MaxResponseBytes int
	MaxOutputTokens  int
}
type RedisConfig struct {
	Host         string
	Port         int
	Username     string
	Password     string
	DB           int
	TLS          bool
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (r RedisConfig) Address() string { return net.JoinHostPort(r.Host, strconv.Itoa(r.Port)) }

type TasksConfig struct {
	Queue                    string
	Concurrency              int
	MaxRounds                int
	RetryInitial             time.Duration
	RetryMax                 time.Duration
	ShutdownTimeout          time.Duration
	OutboxPollInterval       time.Duration
	CancellationPollInterval time.Duration
}

func loadAIConfig(c *Config, values map[string]string) error {
	var err error
	duration := func(path string, target *time.Duration, fallback time.Duration) {
		if err == nil {
			*target, err = durationOr(environmentKey(path), values, fallback)
		}
	}
	integer := func(path string, target *int, fallback int) {
		if err == nil {
			*target, err = positiveIntOr(environmentKey(path), values, fallback)
		}
	}
	duration("ai.round_timeout", &c.AI.RoundTimeout, 5*time.Minute)
	duration("ai.routing.cooldown", &c.AI.Cooldown, time.Minute)
	integer("ai.routing.failure_threshold", &c.AI.FailureThreshold, 3)
	integer("ai.routing.max_response_bytes", &c.AI.MaxResponseBytes, 2<<20)
	integer("ai.routing.max_output_tokens", &c.AI.MaxOutputTokens, 4096)
	if err != nil {
		return err
	}
	c.Redis.Host = envOr("SHIFTORY_REDIS_HOST", values, "127.0.0.1")
	c.Redis.Username = envOr("SHIFTORY_REDIS_USERNAME", values, "")
	c.Redis.Password = secretOr("SHIFTORY_REDIS_PASSWORD", values, "")
	c.Redis.Port, err = portOr("SHIFTORY_REDIS_PORT", values, 6379)
	if err != nil {
		return err
	}
	c.Redis.DB, err = strconv.Atoi(envOr("SHIFTORY_REDIS_DB", values, "0"))
	if err != nil || c.Redis.DB < 0 || c.Redis.DB > 15 {
		return errors.New("invalid Redis DB")
	}
	c.Redis.TLS, err = boolOr("SHIFTORY_REDIS_TLS", values, false)
	if err != nil {
		return err
	}
	duration("redis.dial_timeout", &c.Redis.DialTimeout, 5*time.Second)
	duration("redis.read_timeout", &c.Redis.ReadTimeout, 5*time.Second)
	duration("redis.write_timeout", &c.Redis.WriteTimeout, 5*time.Second)
	c.Tasks.Queue = envOr("SHIFTORY_TASKS_QUEUE", values, "ai_import")
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(c.Tasks.Queue) {
		return errors.New("invalid task queue")
	}
	integer("tasks.concurrency", &c.Tasks.Concurrency, 2)
	integer("tasks.max_rounds", &c.Tasks.MaxRounds, 3)
	duration("tasks.retry_initial", &c.Tasks.RetryInitial, 5*time.Second)
	duration("tasks.retry_max", &c.Tasks.RetryMax, 5*time.Minute)
	duration("tasks.shutdown_timeout", &c.Tasks.ShutdownTimeout, 330*time.Second)
	duration("tasks.outbox_poll_interval", &c.Tasks.OutboxPollInterval, 2*time.Second)
	duration("tasks.cancellation_poll_interval", &c.Tasks.CancellationPollInterval, 2*time.Second)
	if err != nil {
		return err
	}
	if c.Tasks.ShutdownTimeout <= c.AI.RoundTimeout || c.Tasks.RetryMax < c.Tasks.RetryInitial {
		return errors.New("invalid task timeout or retry budget")
	}
	raw := values["SHIFTORY_AI_PROVIDERS"]
	if raw != "" {
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&c.AI.Providers) != nil {
			return errors.New("invalid AI provider configuration")
		}
	}
	ids, orders := map[string]bool{}, map[int]bool{}
	var total time.Duration
	for i := range c.AI.Providers {
		p := &c.AI.Providers[i]
		if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(p.ID) || strings.EqualFold(p.ID, "auto") || strings.EqualFold(p.ID, "all") || ids[strings.ToUpper(p.ID)] {
			return errors.New("invalid or duplicate AI provider id")
		}
		ids[strings.ToUpper(p.ID)] = true
		// 环境覆盖只作用于已定义实例，不允许通过覆盖创建模型
		data, _ := json.Marshal(p)
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(data, &fields)
		for k, v := range fields {
			if k == "id" {
				continue
			}
			name := "SHIFTORY_AI_PROVIDERS_" + strings.ToUpper(p.ID) + "_" + strings.ToUpper(k)
			if override := os.Getenv(name); strings.TrimSpace(override) != "" {
				switch string(v) {
				case "true", "false":
					b, e := strconv.ParseBool(override)
					if e != nil {
						return fmt.Errorf("invalid %s", name)
					}
					fields[k], _ = json.Marshal(b)
				default:
					if k == "order" || k == "max_concurrency" {
						n, e := strconv.Atoi(override)
						if e != nil {
							return fmt.Errorf("invalid %s", name)
						}
						fields[k], _ = json.Marshal(n)
					} else {
						fields[k], _ = json.Marshal(override)
					}
				}
			}
		}
		data, _ = json.Marshal(fields)
		_ = json.Unmarshal(data, p)
		if p.Order <= 0 || orders[p.Order] {
			return errors.New("AI order must be unique and positive")
		}
		orders[p.Order] = true
		if (p.Supplier == "doubao" && p.Kind != "ark") || ((p.Supplier == "relay" || p.Supplier == "deepseek") && p.Kind != "openai") || (p.Supplier != "doubao" && p.Supplier != "relay" && p.Supplier != "deepseek") {
			return errors.New("invalid AI supplier or adapter")
		}
		if p.ResponseFormat != "prompt" && p.ResponseFormat != "json_object" && p.ResponseFormat != "json_schema" {
			return errors.New("invalid response format")
		}
		if p.Thinking != "" && !(p.Kind == "ark" && (p.Thinking == "enabled" || p.Thinking == "disabled")) {
			return errors.New("invalid thinking parameter")
		}
		timeout, e := time.ParseDuration(p.RequestTimeout)
		if e != nil || timeout <= 0 || p.MaxConcurrency <= 0 {
			return errors.New("invalid provider limits")
		}
		if p.Enabled {
			total += timeout
			if c.AIEnabled && (p.APIKey == "" || p.Model == "" || (!p.ImageEnabled && !p.TextEnabled)) {
				return errors.New("enabled AI provider is incomplete")
			}
			u, e := url.Parse(p.BaseURL)
			if c.AIEnabled && (e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "") {
				return errors.New("invalid AI endpoint")
			}
		}
	}
	if total >= c.AI.RoundTimeout {
		return errors.New("AI round timeout must exceed sum of request timeouts")
	}
	sort.Slice(c.AI.Providers, func(i, j int) bool { return c.AI.Providers[i].Order < c.AI.Providers[j].Order })
	return nil
}

func (c AIConfig) Supports(image bool) bool {
	for _, p := range c.Providers {
		if p.Enabled && ((image && p.ImageEnabled) || (!image && p.TextEnabled)) {
			return true
		}
	}
	return false
}
