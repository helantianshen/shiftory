package config

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Provider struct {
	ID             string `json:"id" mapstructure:"id"`
	Supplier       string `json:"supplier" mapstructure:"supplier"`
	Kind           string `json:"kind" mapstructure:"kind"`
	Enabled        bool   `json:"enabled" mapstructure:"enabled"`
	Order          int    `json:"order" mapstructure:"order"`
	BaseURL        string `json:"base_url" mapstructure:"base_url"`
	APIKey         string `json:"api_key" mapstructure:"api_key"`
	Model          string `json:"model" mapstructure:"model"`
	ImageEnabled   bool   `json:"image_enabled" mapstructure:"image_enabled"`
	TextEnabled    bool   `json:"text_enabled" mapstructure:"text_enabled"`
	ResponseFormat string `json:"response_format" mapstructure:"response_format"`
	Thinking       string `json:"thinking" mapstructure:"thinking"`
	RequestTimeout string `json:"request_timeout" mapstructure:"request_timeout"`
	MaxConcurrency int    `json:"max_concurrency" mapstructure:"max_concurrency"`
}
type AIConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	Model          string        `mapstructure:"model"`
	BaseURL        string        `mapstructure:"base_url"`
	APIKey         string        `mapstructure:"api_key"`
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	Providers      []Provider    `mapstructure:"providers"`
	RoundTimeout   time.Duration `mapstructure:"round_timeout"`
	Routing        RoutingConfig `mapstructure:"routing"`
}

type RoutingConfig struct {
	FailureThreshold int           `mapstructure:"failure_threshold"`
	Cooldown         time.Duration `mapstructure:"cooldown"`
	MaxResponseBytes int           `mapstructure:"max_response_bytes"`
	MaxOutputTokens  int           `mapstructure:"max_output_tokens"`
}

type RedisConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Username     string        `mapstructure:"username"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	TLS          bool          `mapstructure:"tls"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

func (r RedisConfig) Address() string { return net.JoinHostPort(r.Host, strconv.Itoa(r.Port)) }

type TasksConfig struct {
	Queue                    string        `mapstructure:"queue"`
	Concurrency              int           `mapstructure:"concurrency"`
	MaxRounds                int           `mapstructure:"max_rounds"`
	RetryInitial             time.Duration `mapstructure:"retry_initial"`
	RetryMax                 time.Duration `mapstructure:"retry_max"`
	ShutdownTimeout          time.Duration `mapstructure:"shutdown_timeout"`
	OutboxPollInterval       time.Duration `mapstructure:"outbox_poll_interval"`
	CancellationPollInterval time.Duration `mapstructure:"cancellation_poll_interval"`
}

func validateProviders(c *Config) error {
	ids, orders := map[string]bool{}, map[int]bool{}
	var total time.Duration
	for i := range c.AI.Providers {
		p := &c.AI.Providers[i]
		if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(p.ID) || strings.EqualFold(p.ID, "auto") || strings.EqualFold(p.ID, "all") || ids[strings.ToUpper(p.ID)] {
			return errors.New("invalid or duplicate AI provider id")
		}
		ids[strings.ToUpper(p.ID)] = true
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
			if c.AI.Enabled && (p.APIKey == "" || p.Model == "" || (!p.ImageEnabled && !p.TextEnabled)) {
				return errors.New("enabled AI provider is incomplete")
			}
			u, e := url.Parse(p.BaseURL)
			if c.AI.Enabled && (e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "") {
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
