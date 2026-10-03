// Package config 加载后端启动参数、YAML 与进程环境变量，并在启动前校验配置
package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Config 保存完成来源合并与类型校验后的后端启动配置
type Config struct {
	MigrateLegacyAI      bool
	AI                   AIConfig
	Redis                RedisConfig
	Tasks                TasksConfig
	Environment          string
	LogLevel             string
	LogFormat            string
	Port                 int
	Postgres             PostgresConfig
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

// PostgresConfig 保存可由部署环境调整的数据库连接参数
type PostgresConfig struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string
}

// DSN 编码连接参数并固定会话时区，密码及数据库名称中的特殊字符保留原值
func (c PostgresConfig) DSN() string {
	value := url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password), Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Database}
	params := url.Values{"sslmode": {c.SSLMode}, "timezone": {"UTC"}}
	value.RawQuery = params.Encode()
	return value.String()
}

// Address 将已校验的服务端口转换为监听所有网卡的地址
func (c Config) Address() string {
	return net.JoinHostPort("", strconv.Itoa(c.Port))
}

// Load 根据启动参数加载配置，未指定参数时使用 production 模式
// 配置优先级为非空进程环境变量、YAML、内置默认值，加载过程不会修改进程环境变量
func Load(args ...string) (Config, error) {
	// 模式仅由命令行决定，额外位置参数视为启动错误
	flags := flag.NewFlagSet("shiftory", flag.ContinueOnError)
	environmentFlag := flags.String("env", "production", "configuration profile: development or production")
	legacyFlag := flags.Bool("migrate-ai-jobs", false, "enqueue legacy AI jobs after stopping all old API processes")
	configPath := flags.String("config", "", "explicit YAML configuration file (optional)")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	environment := strings.ToLower(strings.TrimSpace(*environmentFlag))
	if environment != "development" && environment != "production" {
		return Config{}, fmt.Errorf("invalid --env %q: expected development or production", environment)
	}
	fileValues, err := loadYAMLFile(environment, strings.TrimSpace(*configPath))
	if err != nil {
		return Config{}, err
	}
	// 模式提供日志默认值，各项仍允许进程环境变量或 YAML 覆盖
	defaultLogLevel, defaultLogFormat := "info", "json"
	if environment == "development" {
		defaultLogLevel, defaultLogFormat = "debug", "text"
	}
	logLevel := strings.ToLower(envOr("SHIFTORY_LOG_LEVEL", fileValues, defaultLogLevel))
	if !validLogLevel(logLevel) {
		return Config{}, fmt.Errorf("invalid SHIFTORY_LOG_LEVEL %q: expected debug, info, warn, or error", logLevel)
	}
	logFormat := strings.ToLower(envOr("SHIFTORY_LOG_FORMAT", fileValues, defaultLogFormat))
	if logFormat != "text" && logFormat != "json" {
		return Config{}, fmt.Errorf("invalid SHIFTORY_LOG_FORMAT %q: expected text or json", logFormat)
	}
	// 时间与并发配置先完成类型和正值校验，避免把无效参数传入运行器
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
	aiAPIKey := secretOr("SHIFTORY_AI_API_KEY", fileValues, "")
	aiEnabled, err := boolOr("SHIFTORY_AI_ENABLED", fileValues, false)
	if err != nil {
		return Config{}, err
	}
	port, err := portOr("SHIFTORY_SERVER_PORT", fileValues, 8080)
	if err != nil {
		return Config{}, err
	}
	postgresPort, err := portOr("SHIFTORY_POSTGRES_PORT", fileValues, 5432)
	if err != nil {
		return Config{}, err
	}
	sslmode := envOr("SHIFTORY_POSTGRES_SSLMODE", fileValues, "disable")
	switch sslmode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return Config{}, fmt.Errorf("invalid SHIFTORY_POSTGRES_SSLMODE %q", sslmode)
	}

	// 把已校验的参数与按优先级解析的字符串组装为统一启动配置
	cfg := Config{
		MigrateLegacyAI: *legacyFlag,
		Environment:     environment,
		LogLevel:        logLevel,
		LogFormat:       logFormat,
		Port:            port,
		Postgres: PostgresConfig{
			Host:     envOr("SHIFTORY_POSTGRES_HOST", fileValues, "127.0.0.1"),
			Port:     postgresPort,
			Database: envOr("SHIFTORY_POSTGRES_DATABASE", fileValues, "shiftory"),
			User:     envOr("SHIFTORY_POSTGRES_USER", fileValues, "shiftory"),
			Password: secretOr("SHIFTORY_POSTGRES_PASSWORD", fileValues, "123456"),
			SSLMode:  envOr("SHIFTORY_POSTGRES_SSLMODE", fileValues, "disable"),
		},
		UploadDir:            envOr("SHIFTORY_STORAGE_UPLOAD_DIR", fileValues, "./uploads"),
		WebDir:               envOr("SHIFTORY_SERVER_WEB_DIR", fileValues, "../shiftory-web/dist"),
		PublicOrigin:         envOr("SHIFTORY_SERVER_PUBLIC_ORIGIN", fileValues, "http://localhost:5173"),
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
	if err := loadAIConfig(&cfg, fileValues); err != nil {
		return Config{}, err
	}
	// 公开来源必须是无路径的 HTTP 或 HTTPS 地址，用于来源及 Cookie 策略
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" {
		return Config{}, fmt.Errorf("invalid SHIFTORY_SERVER_PUBLIC_ORIGIN %q", cfg.PublicOrigin)
	}
	return cfg, nil
}

// validLogLevel 判断日志等级是否属于支持的四种取值
func validLogLevel(value string) bool {
	switch value {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

// envOr 按非空进程环境变量、YAML 和默认值的优先级读取字符串
func envOr(name string, fileValues map[string]string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	if value := strings.TrimSpace(fileValues[name]); value != "" {
		return value
	}
	return fallback
}

// secretOr 保留密钥中的空白字符，YAML 中显式空字符串可表示无密码
func secretOr(name string, fileValues map[string]string, fallback string) string {
	if value := os.Getenv(name); strings.TrimSpace(value) != "" {
		return value
	}
	if value, ok := fileValues[name]; ok {
		return value
	}
	return fallback
}

// portOr 读取 TCP 端口并限制在有效端口范围内
func portOr(name string, fileValues map[string]string, fallback int) (int, error) {
	value, err := positiveIntOr(name, fileValues, fallback)
	if err != nil {
		return 0, err
	}
	if value > 65535 {
		return 0, fmt.Errorf("invalid %s: expected a port from 1 to 65535", name)
	}
	return value, nil
}

// durationOr 读取带单位的正时间间隔，空配置使用默认值
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

// positiveIntOr 读取正整数配置，拒绝零、负数及非法数字
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

// boolOr 读取布尔配置，显式 false 可覆盖默认值或文件值
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

// yamlKeys 明确列出可用的 YAML 键，未知键必须报错，避免拼写错误静默回退到默认值
var yamlKeys = []string{
	"log.level", "log.format", "server.port", "server.web_dir", "server.public_origin",
	"postgres.host", "postgres.port", "postgres.database", "postgres.user", "postgres.password", "postgres.sslmode",
	"storage.upload_dir", "jwt.issuer", "jwt.audience", "jwt.private_key_file", "jwt.public_key_file",
	"ai.model", "ai.base_url", "ai.api_key", "ai.enabled", "ai.request_timeout",
	"ai.providers", "ai.round_timeout", "ai.routing.failure_threshold", "ai.routing.cooldown", "ai.routing.max_response_bytes", "ai.routing.max_output_tokens",
	"redis.host", "redis.port", "redis.username", "redis.password", "redis.db", "redis.tls", "redis.dial_timeout", "redis.read_timeout", "redis.write_timeout",
	"tasks.queue", "tasks.concurrency", "tasks.max_rounds", "tasks.retry_initial", "tasks.retry_max", "tasks.shutdown_timeout", "tasks.outbox_poll_interval", "tasks.cancellation_poll_interval",
	"worker.id", "worker.lease", "worker.poll_interval", "worker.max_concurrency",
}

// environmentKey 将 YAML 字段路径转换为带应用前缀的分组环境变量名称
func environmentKey(path string) string {
	return "SHIFTORY_" + strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
}

// loadYAMLFile 加载显式文件或逐级发现首个模式配置，未发现时返回空映射
func loadYAMLFile(environment, explicit string) (map[string]string, error) {
	if explicit != "" {
		return parseYAMLFile(explicit)
	}
	// 自动发现从进程工作目录向上检查三层，首个匹配文件生效且不与其他文件合并
	directory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("locate configuration directory: %w", err)
	}
	for depth := 0; depth < 3; depth++ {
		candidate := filepath.Join(directory, "config", environment+".yaml")
		if _, err := os.Stat(candidate); err == nil {
			return parseYAMLFile(candidate)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect YAML configuration %s: %w", candidate, err)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return map[string]string{}, nil
}

// parseYAMLFile 解析单文档分组映射，校验允许的键并转换为环境变量名称
func parseYAMLFile(path string) (map[string]string, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".yaml" && extension != ".yml" {
		return nil, fmt.Errorf("configuration file must use .yaml or .yml: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read YAML configuration %s: %w", path, err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	var values map[string]map[string]any
	if err := decoder.Decode(&values); err != nil || values == nil {
		// 解码器错误可能包含带凭据的原文片段，因此这里只返回固定错误信息
		return nil, fmt.Errorf("invalid YAML configuration %s: expected a single mapping of configuration groups", path)
	}
	// 只接受单份 YAML 文档，避免多文档内容被静默忽略
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid YAML configuration %s: expected exactly one document", path)
	}
	// 白名单校验后统一使用环境变量键名，后续读取无需区分文件与进程来源
	allowed := make(map[string]bool, len(yamlKeys))
	for _, key := range yamlKeys {
		allowed[key] = true
	}
	result := make(map[string]string, len(values))
	for group, fields := range values {
		knownGroup := false
		for _, key := range yamlKeys {
			if strings.HasPrefix(key, group+".") {
				knownGroup = true
				break
			}
		}
		if !knownGroup || fields == nil {
			return nil, fmt.Errorf("invalid YAML configuration group %q in %s", group, path)
		}
		for field, value := range fields {
			key := group + "." + field
			if key == "ai.providers" {
				b, e := json.Marshal(value)
				if e != nil {
					return nil, errors.New("invalid providers")
				}
				result[environmentKey(key)] = string(b)
				continue
			}
			if key == "ai.routing" {
				nested, ok := value.(map[string]any)
				if !ok {
					return nil, errors.New("invalid ai.routing")
				}
				for k, v := range nested {
					path := key + "." + k
					if !allowed[path] || v == nil {
						return nil, errors.New("invalid routing field")
					}
					result[environmentKey(path)] = fmt.Sprint(v)
				}
				continue
			}
			if !allowed[key] {
				return nil, fmt.Errorf("unknown YAML configuration key %q in %s", key, path)
			}
			if value == nil {
				return nil, fmt.Errorf("null YAML configuration value %q in %s", key, path)
			}
			switch value.(type) {
			case string, bool, int, int64, uint64, float64:
			default:
				return nil, errors.New("configuration value must be scalar")
			}
			result[environmentKey(key)] = fmt.Sprint(value)
		}
	}
	return result, nil
}
