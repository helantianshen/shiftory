// Package config 将启动参数、YAML 与进程环境变量加载为一次性配置结构体
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// Config 保存启动时完成来源合并和校验的配置，运行期间不重新读取
type Config struct {
	Environment     string         `mapstructure:"-"`
	MigrateLegacyAI bool           `mapstructure:"-"`
	Log             LogConfig      `mapstructure:"log"`
	Server          ServerConfig   `mapstructure:"server"`
	Postgres        PostgresConfig `mapstructure:"postgres"`
	JWT             JWTConfig      `mapstructure:"jwt"`
	Storage         StorageConfig  `mapstructure:"storage"`
	AI              AIConfig       `mapstructure:"ai"`
	Redis           RedisConfig    `mapstructure:"redis"`
	Tasks           TasksConfig    `mapstructure:"tasks"`
	Worker          WorkerConfig   `mapstructure:"worker"`
	AccessTokenTTL  time.Duration  `mapstructure:"-"`
	RefreshTokenTTL time.Duration  `mapstructure:"-"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	WebDir       string `mapstructure:"web_dir"`
	PublicOrigin string `mapstructure:"public_origin"`
}

type PostgresConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Database string `mapstructure:"database"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"sslmode"`
}

type JWTConfig struct {
	Issuer         string `mapstructure:"issuer"`
	Audience       string `mapstructure:"audience"`
	PrivateKeyFile string `mapstructure:"private_key_file"`
	PublicKeyFile  string `mapstructure:"public_key_file"`
}

type StorageConfig struct {
	UploadDir string `mapstructure:"upload_dir"`
}

// WorkerConfig 保留旧版 YAML 中的 Worker 配置字段
type WorkerConfig struct {
	ID             string        `mapstructure:"id"`
	Lease          time.Duration `mapstructure:"lease"`
	PollInterval   time.Duration `mapstructure:"poll_interval"`
	MaxConcurrency int           `mapstructure:"max_concurrency"`
}

// DSN 编码连接参数并固定会话时区，密码及数据库名称中的特殊字符保留原值
func (c PostgresConfig) DSN() string {
	value := url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password), Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Database}
	value.RawQuery = url.Values{"sslmode": {c.SSLMode}, "timezone": {"UTC"}}.Encode()
	return value.String()
}

func (c Config) Address() string { return net.JoinHostPort("", strconv.Itoa(c.Server.Port)) }

// Load 按非空进程环境变量、YAML、默认值的优先级加载配置，不修改进程环境变量
func Load(args ...string) (Config, error) {
	flags := flag.NewFlagSet("shiftory", flag.ContinueOnError)
	env := flags.String("env", "production", "configuration profile: development or production")
	legacy := flags.Bool("migrate-ai-jobs", false, "enqueue legacy AI jobs after stopping all old API processes")
	path := flags.String("config", "", "explicit YAML configuration file (optional)")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, errors.New("unexpected positional arguments")
	}
	environment := strings.ToLower(strings.TrimSpace(*env))
	if environment != "production" && environment != "development" {
		return Config{}, errors.New("invalid --env: expected development or production")
	}

	v := viper.New()
	defaults := defaultValues(environment)
	for key, value := range defaults {
		v.SetDefault(key, value)
		bindEnvironment(v, key, environmentKey(key))
	}
	if err := readConfiguration(v, environment, strings.TrimSpace(*path)); err != nil {
		return Config{}, err
	}
	// 普通空字符串沿用默认值，密码和密钥保留原始内容及显式空值
	for _, key := range v.AllKeys() {
		if value, ok := v.Get(key).(string); ok && !secretKey(key) && key != "ai.providers" {
			value = strings.TrimSpace(value)
			if value == "" {
				v.Set(key, defaults[key])
			} else {
				v.Set(key, value)
			}
		}
	}
	if raw := os.Getenv("SHIFTORY_AI_PROVIDERS"); strings.TrimSpace(raw) != "" {
		var providers []map[string]any
		if err := json.Unmarshal([]byte(raw), &providers); err != nil {
			return Config{}, errors.New("invalid SHIFTORY_AI_PROVIDERS: expected a JSON array")
		}
		v.Set("ai.providers", providers)
	}
	cfg := Config{Environment: environment, MigrateLegacyAI: *legacy, AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 7 * 24 * time.Hour}
	if err := decodeConfiguration(v, &cfg); err != nil {
		return Config{}, err
	}
	for i := range cfg.AI.Providers {
		p := &cfg.AI.Providers[i]
		values := map[string]any{}
		if err := mapstructure.Decode(p, &values); err != nil {
			return Config{}, errors.New("invalid AI provider configuration")
		}
		provider := viper.New()
		for key, value := range values {
			provider.SetDefault(key, value)
			if key != "id" {
				bindEnvironment(provider, key, "SHIFTORY_AI_PROVIDERS_"+strings.ToUpper(p.ID)+"_"+strings.ToUpper(key))
			}
		}
		if err := decodeConfiguration(provider, p); err != nil {
			return Config{}, fmt.Errorf("invalid AI provider %s configuration", p.ID)
		}
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultValues(environment string) map[string]any {
	level, format := "info", "json"
	if environment == "development" {
		level, format = "debug", "text"
	}
	return map[string]any{
		"log.level": level, "log.format": format,
		"server.port": 8080, "server.web_dir": "../shiftory-web/dist", "server.public_origin": "http://localhost:5173",
		"postgres.host": "127.0.0.1", "postgres.port": 5432, "postgres.database": "shiftory", "postgres.user": "shiftory", "postgres.password": "123456", "postgres.sslmode": "disable",
		"storage.upload_dir": "./uploads", "jwt.issuer": "shiftory-local", "jwt.audience": "shiftory-web", "jwt.private_key_file": "./var/jwt-private.pem", "jwt.public_key_file": "./var/jwt-public.pem",
		"ai.enabled": false, "ai.model": "", "ai.base_url": "", "ai.api_key": "", "ai.request_timeout": 90 * time.Second,
		"ai.providers": []Provider{}, "ai.round_timeout": 5 * time.Minute,
		"ai.routing.failure_threshold": 3, "ai.routing.cooldown": time.Minute, "ai.routing.max_response_bytes": 2 << 20, "ai.routing.max_output_tokens": 4096,
		"redis.host": "127.0.0.1", "redis.port": 6379, "redis.username": "", "redis.password": "", "redis.db": 0, "redis.tls": false,
		"redis.dial_timeout": 5 * time.Second, "redis.read_timeout": 5 * time.Second, "redis.write_timeout": 5 * time.Second,
		"tasks.queue": "ai_import", "tasks.concurrency": 2, "tasks.max_rounds": 3, "tasks.retry_initial": 5 * time.Second, "tasks.retry_max": 5 * time.Minute, "tasks.shutdown_timeout": 330 * time.Second,
		"tasks.outbox_poll_interval": 2 * time.Second, "tasks.cancellation_poll_interval": 2 * time.Second,
		"worker.id": "shiftory-worker-local", "worker.lease": 2 * time.Minute, "worker.poll_interval": 2 * time.Second, "worker.max_concurrency": 2,
	}
}

func environmentKey(path string) string {
	return "SHIFTORY_" + strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
}
func secretKey(key string) bool {
	return strings.HasSuffix(key, ".password") || strings.HasSuffix(key, ".api_key")
}
func bindEnvironment(v *viper.Viper, key, name string) {
	if strings.TrimSpace(os.Getenv(name)) != "" {
		_ = v.BindEnv(key, name)
	}
}

// decodeConfiguration 使用严格解码拒绝未知字段、非整数和非法布尔值，错误不包含配置原文
func decodeConfiguration(v *viper.Viper, target any) error {
	hook := func(from, to reflect.Type, data any) (any, error) {
		if to == reflect.TypeFor[time.Duration]() {
			if from == to {
				return data, nil
			}
			return time.ParseDuration(fmt.Sprint(data))
		}
		switch to.Kind() {
		case reflect.Int:
			return strconv.Atoi(fmt.Sprint(data))
		case reflect.Bool:
			return strconv.ParseBool(fmt.Sprint(data))
		case reflect.String:
			switch from.Kind() {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Int64, reflect.Uint64, reflect.Float64:
				return fmt.Sprint(data), nil
			}
		}
		return data, nil
	}
	if err := v.UnmarshalExact(target, viper.DecodeHook(hook), func(c *mapstructure.DecoderConfig) { c.WeaklyTypedInput = false }); err != nil {
		return errors.New("invalid configuration: unknown field or invalid value type")
	}
	return nil
}

// readConfiguration 只读取首个匹配文件，显式缺失文件和非单文档映射均拒绝加载
func readConfiguration(v *viper.Viper, environment, path string) error {
	if path == "" {
		directory, err := os.Getwd()
		if err != nil {
			return err
		}
		for depth := 0; depth < 3; depth++ {
			candidate := filepath.Join(directory, "config", environment+".yaml")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect configuration: %w", err)
			}
			directory = filepath.Dir(directory)
		}
		if path == "" {
			return nil
		}
	}
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".yaml" && extension != ".yml" {
		return errors.New("configuration file must use .yaml or .yml")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document, extra yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("invalid YAML configuration: expected a mapping")
	}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid YAML configuration: expected exactly one document")
	}
	allowed := v.AllKeys()
	var checkKeys func(*yaml.Node, string) bool
	checkKeys = func(node *yaml.Node, path string) bool {
		if node.Kind == yaml.AliasNode {
			node = node.Alias
		}
		for _, key := range allowed {
			if key == path {
				if path == "ai.providers" {
					return node.Kind == yaml.SequenceNode
				}
				return node.Kind == yaml.ScalarNode
			}
		}
		knownGroup := path == ""
		for _, key := range allowed {
			if strings.HasPrefix(key, path+".") {
				knownGroup = true
				break
			}
		}
		if !knownGroup || node.Kind != yaml.MappingNode {
			return false
		}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if path != "" {
				key = path + "." + key
			}
			if !checkKeys(node.Content[i+1], key) {
				return false
			}
		}
		return true
	}
	if !checkKeys(document.Content[0], "") {
		return errors.New("invalid YAML configuration: unknown field or invalid group")
	}
	var checkValues func(*yaml.Node) bool
	checkValues = func(node *yaml.Node) bool {
		if node.Tag == "!!null" {
			return false
		}
		for _, child := range node.Content {
			if !checkValues(child) {
				return false
			}
		}
		return true
	}
	if !checkValues(&document) {
		return errors.New("invalid YAML configuration: null values are not supported")
	}
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return errors.New("invalid YAML configuration")
	}
	return nil
}
