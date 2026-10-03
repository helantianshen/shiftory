package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/goccy/go-yaml"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	_ "golang.org/x/image/webp"
)

type Config struct {
	AI struct {
		Enabled      bool       `yaml:"enabled"`
		RoundTimeout string     `yaml:"round_timeout"`
		Providers    []Provider `yaml:"providers"`
		Routing      struct {
			FailureThreshold int    `yaml:"failure_threshold"`
			Cooldown         string `yaml:"cooldown"`
			MaxResponseBytes int64  `yaml:"max_response_bytes"`
			MaxOutputTokens  int    `yaml:"max_output_tokens"`
		} `yaml:"routing"`
	} `yaml:"ai"`
	Redis struct {
		Host         string `yaml:"host"`
		Port         int    `yaml:"port"`
		Username     string `yaml:"username"`
		Password     string `yaml:"password"`
		DB           int    `yaml:"db"`
		TLS          bool   `yaml:"tls"`
		DialTimeout  string `yaml:"dial_timeout"`
		ReadTimeout  string `yaml:"read_timeout"`
		WriteTimeout string `yaml:"write_timeout"`
	} `yaml:"redis"`
	Tasks struct {
		Queue                    string `yaml:"queue"`
		Concurrency              int    `yaml:"concurrency"`
		MaxRounds                int    `yaml:"max_rounds"`
		RetryInitial             string `yaml:"retry_initial"`
		RetryMax                 string `yaml:"retry_max"`
		ShutdownTimeout          string `yaml:"shutdown_timeout"`
		OutboxPollInterval       string `yaml:"outbox_poll_interval"`
		CancellationPollInterval string `yaml:"cancellation_poll_interval"`
	} `yaml:"tasks"`
}

type Provider struct {
	ID             string `yaml:"id"`
	Supplier       string `yaml:"supplier"`
	Kind           string `yaml:"kind"`
	Enabled        bool   `yaml:"enabled"`
	Order          int    `yaml:"order"`
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	Model          string `yaml:"model"`
	ImageEnabled   bool   `yaml:"image_enabled"`
	TextEnabled    bool   `yaml:"text_enabled"`
	ResponseFormat string `yaml:"response_format"`
	Thinking       string `yaml:"thinking"`
	RequestTimeout string `yaml:"request_timeout"`
	MaxConcurrency int    `yaml:"max_concurrency"`
}

// 接入测试使用简化的返回契约，不替代正式规则解析和排班领域校验
const outputSchema = `{"type":"object","properties":{"summary":{"type":"string"},"issues":{"type":"array","items":{"type":"string"}}},"required":["summary","issues"],"additionalProperties":false}`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("config", "../../config/ai.development.yaml", "AI 开发配置路径")
	live := flag.Bool("run", false, "实际调用供应商，可能消耗额度")
	selected := flag.String("provider", "auto", "供应商模型 id、auto 或 all")
	imagePath := flag.String("image", "", "本地图片路径，空值表示文字测试")
	text := flag.String("text", "2026年10月，每周一至周五9:00至18:00上班，周末休息。请概括规则并列出需要确认的疑问。", "测试描述或图片识别要求")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("不接受位置参数")
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		return err
	}
	for _, p := range cfg.AI.Providers {
		state := "disabled"
		if p.Enabled {
			state = "ready"
			if !ready(p) {
				state = "needs_credentials_or_endpoint_or_model"
			}
		}
		fmt.Printf("id=%s supplier=%s order=%d adapter=%s status=%s\n", p.ID, p.Supplier, p.Order, p.Kind, state)
	}
	if !*live {
		fmt.Println("配置检查通过；尚未调用模型、Redis 或 PostgreSQL。填写配置后使用 -run 调用模型")
		return nil
	}
	if !cfg.AI.Enabled {
		return errors.New("ai.enabled 为 false，拒绝调用")
	}
	if strings.TrimSpace(*text) == "" {
		return errors.New("测试描述不能为空")
	}
	messages := []*schema.Message{
		schema.SystemMessage("你为一个由系统确定的成员提取排班信息。目标成员身份已经确定。只概括输入中的日期、工作或休息状态、班次时间和跨日规则，不设计人员配置或考勤制度。issues 只记录阻止确定上述排班信息的疑问，不要求人员名单、工号、岗位人数、薪酬或补休政策；已经明确的信息不能列为疑问。不得编造无法辨认的内容。仅返回符合以下 JSON Schema 的 JSON，不使用 Markdown。" + outputSchema),
		schema.UserMessage(*text),
	}
	if *imagePath != "" {
		part, err := imagePart(*imagePath)
		if err != nil {
			return err
		}
		messages[1].Content = ""
		messages[1].UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: *text}, part}
	}
	candidates, err := candidates(cfg, *selected, *imagePath != "")
	if err != nil {
		return err
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	roundTimeout, _ := time.ParseDuration(cfg.AI.RoundTimeout)
	ctx, cancel := context.WithTimeout(root, roundTimeout)
	defer cancel()
	successes := 0
	for _, p := range candidates {
		if ctx.Err() != nil {
			return errors.New("测试已取消或整轮超时")
		}
		started := time.Now()
		response, err := call(ctx, p, cfg, messages)
		if err != nil {
			fmt.Printf("id=%s elapsed=%s result=failed reason=%s\n", p.ID, time.Since(started).Round(time.Millisecond), err)
			continue
		}
		successes++
		for _, secret := range cfg.AI.Providers {
			if secret.APIKey != "" {
				response = strings.ReplaceAll(response, secret.APIKey, "[REDACTED]")
			}
		}
		fmt.Printf("id=%s elapsed=%s result=success\n%s\n", p.ID, time.Since(started).Round(time.Millisecond), response)
		if *selected != "all" {
			return nil
		}
	}
	if *selected == "all" && successes == len(candidates) {
		return nil
	}
	return errors.New("模型测试未全部成功；已隐藏上游错误正文，请检查端点、权限、协议及输出配置")
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	file, err := os.Open(path)
	if err != nil {
		return cfg, errors.New("无法读取 AI 开发配置文件")
	}
	defer file.Close()
	decoder := yaml.NewDecoder(io.LimitReader(file, 1<<20), yaml.Strict())
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, errors.New("AI YAML 配置无效或包含未知字段，错误正文已隐藏")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, errors.New("AI 配置必须仅包含一个 YAML 文档")
	}
	if len(cfg.AI.Providers) == 0 {
		return cfg, errors.New("ai.providers 不能为空")
	}
	ids, orders := map[string]bool{}, map[int]bool{}
	var total time.Duration
	for _, p := range cfg.AI.Providers {
		if !validID(p.ID) || strings.EqualFold(p.ID, "auto") || strings.EqualFold(p.ID, "all") || ids[strings.ToUpper(p.ID)] {
			return cfg, errors.New("供应商 id 只能包含字母、数字和下划线，大小写折叠后必须唯一且不能是 auto 或 all")
		}
		ids[strings.ToUpper(p.ID)] = true
		if p.Order <= 0 || orders[p.Order] {
			return cfg, errors.New("供应商 order 必须是唯一正整数，数值越小优先级越高")
		}
		orders[p.Order] = true
		if p.Supplier != "doubao" && p.Supplier != "relay" && p.Supplier != "deepseek" {
			return cfg, errors.New("供应商类型必须是 doubao、relay 或 deepseek")
		}
		if (p.Supplier == "doubao" && p.Kind != "ark") || (p.Supplier != "doubao" && p.Kind != "openai") {
			return cfg, errors.New("字节模型使用 ark 适配器，其余供应商使用 openai 适配器")
		}
		if p.ResponseFormat != "prompt" && p.ResponseFormat != "json_object" && p.ResponseFormat != "json_schema" {
			return cfg, errors.New("response_format 必须为 prompt、json_object 或 json_schema")
		}
		if p.Thinking != "" && !(p.Kind == "ark" && (p.Thinking == "disabled" || p.Thinking == "enabled")) {
			return cfg, errors.New("thinking 仅支持 Ark 的 enabled 或 disabled")
		}
		if p.MaxConcurrency <= 0 {
			return cfg, errors.New("供应商 max_concurrency 必须为正整数")
		}
		timeout, err := time.ParseDuration(p.RequestTimeout)
		if err != nil || timeout <= 0 {
			return cfg, errors.New("供应商 request_timeout 必须为正时长")
		}
		if p.Enabled {
			total += timeout
		}
		if p.BaseURL != "" {
			u, err := url.Parse(p.BaseURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return cfg, errors.New("base_url 必须为不含凭据、查询参数和片段的 HTTP(S) 地址")
			}
		}
	}
	for _, value := range []string{cfg.AI.RoundTimeout, cfg.AI.Routing.Cooldown, cfg.Redis.DialTimeout, cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout, cfg.Tasks.RetryInitial, cfg.Tasks.RetryMax, cfg.Tasks.ShutdownTimeout, cfg.Tasks.OutboxPollInterval, cfg.Tasks.CancellationPollInterval} {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return cfg, errors.New("AI、Redis 或 tasks 中存在非正时长")
		}
	}
	round, _ := time.ParseDuration(cfg.AI.RoundTimeout)
	shutdown, _ := time.ParseDuration(cfg.Tasks.ShutdownTimeout)
	initial, _ := time.ParseDuration(cfg.Tasks.RetryInitial)
	maxRetry, _ := time.ParseDuration(cfg.Tasks.RetryMax)
	if round <= total || shutdown <= round || initial > maxRetry {
		return cfg, errors.New("整轮超时须大于供应商超时总和，停机等待须大于整轮超时，重试上限须不小于初始值")
	}
	if cfg.AI.Routing.MaxResponseBytes <= 0 || cfg.AI.Routing.MaxOutputTokens <= 0 || cfg.AI.Routing.FailureThreshold <= 0 || cfg.Tasks.Queue == "" || cfg.Tasks.Concurrency <= 0 || cfg.Tasks.MaxRounds <= 0 || cfg.Redis.Host == "" || cfg.Redis.Port < 1 || cfg.Redis.Port > 65535 || cfg.Redis.DB < 0 {
		return cfg, errors.New("AI、Redis 或 tasks 的容量、地址或次数配置无效")
	}
	sort.Slice(cfg.AI.Providers, func(i, j int) bool { return cfg.AI.Providers[i].Order < cfg.AI.Providers[j].Order })
	return cfg, nil
}

func ready(p Provider) bool {
	return strings.TrimSpace(p.APIKey) != "" && strings.TrimSpace(p.Model) != "" && strings.TrimSpace(p.BaseURL) != ""
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func candidates(cfg Config, selected string, image bool) ([]Provider, error) {
	var result []Provider
	found := selected == "auto" || selected == "all"
	for _, p := range cfg.AI.Providers {
		if selected != "auto" && selected != "all" && selected != p.ID {
			continue
		}
		found = true
		if !p.Enabled || !ready(p) || (image && !p.ImageEnabled) || (!image && !p.TextEnabled) {
			if selected != "auto" && selected != "all" {
				return nil, errors.New("选定模型未启用、缺少配置或不支持此次输入")
			}
			continue
		}
		result = append(result, p)
	}
	if !found {
		return nil, errors.New("未找到指定的供应商模型 id")
	}
	if len(result) == 0 {
		return nil, errors.New("没有可调用的供应商，请填写凭据、端点和模型")
	}
	return result, nil
}

func imagePart(path string) (schema.MessageInputPart, error) {
	file, err := os.Open(path)
	if err != nil {
		return schema.MessageInputPart{}, errors.New("无法读取测试图片")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 10<<20 {
		return schema.MessageInputPart{}, errors.New("测试图片必须非空且不超过 10 MiB")
	}
	info, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || info.Width <= 0 || info.Height <= 0 || int64(info.Width)*int64(info.Height) > 25_000_000 {
		return schema.MessageInputPart{}, errors.New("图片必须为 PNG/JPEG/GIF/WebP 且不超过 2500 万像素")
	}
	mime := "image/" + format
	encoded := base64.StdEncoding.EncodeToString(data)
	return schema.MessageInputPart{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &encoded, MIMEType: mime}, Detail: schema.ImageURLDetailHigh}}, nil
}

type boundedTransport struct {
	base   http.RoundTripper
	max    int64
	status atomic.Int32
	format atomic.Value
}

func (t *boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.status.Store(int32(resp.StatusCode))
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, t.max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > t.max {
		return nil, errors.New("模型响应超过大小限制")
	}
	format := "other"
	trimmed := bytes.TrimSpace(data)
	switch {
	case bytes.HasPrefix(trimmed, []byte("<")):
		format = "html_or_xml"
	case bytes.HasPrefix(trimmed, []byte("data:")), bytes.HasPrefix(trimmed, []byte("event:")):
		format = "sse"
	case json.Valid(data):
		format = "json"
	}
	t.format.Store(format)
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

func call(root context.Context, p Provider, cfg Config, messages []*schema.Message) (string, error) {
	timeout, _ := time.ParseDuration(p.RequestTimeout)
	ctx, cancel := context.WithTimeout(root, timeout)
	defer cancel()
	transport := &boundedTransport{base: http.DefaultTransport, max: cfg.AI.Routing.MaxResponseBytes}
	client := &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var cm model.ChatModel
	var err error
	formatJSON := `{"type":"` + p.ResponseFormat + `"}`
	if p.ResponseFormat == "json_schema" {
		formatJSON = `{"type":"json_schema","json_schema":{"name":"schedule_smoke","strict":true,"schema":` + outputSchema + `}}`
	}
	if p.Kind == "ark" {
		zero := 0
		c := &ark.ChatModelConfig{APIKey: p.APIKey, Model: p.Model, BaseURL: p.BaseURL, HTTPClient: client, Timeout: &timeout, RetryTimes: &zero, MaxTokens: &cfg.AI.Routing.MaxOutputTokens}
		if p.Thinking != "" {
			c.Thinking = &arkmodel.Thinking{Type: arkmodel.ThinkingType(p.Thinking)}
		}
		if p.ResponseFormat != "prompt" {
			c.ResponseFormat = &ark.ResponseFormat{}
			if json.Unmarshal([]byte(formatJSON), c.ResponseFormat) != nil {
				return "", errors.New("输出协议配置无效")
			}
		}
		cm, err = ark.NewChatModel(ctx, c)
	} else {
		c := &openai.ChatModelConfig{APIKey: p.APIKey, Model: p.Model, BaseURL: p.BaseURL, HTTPClient: client, MaxTokens: &cfg.AI.Routing.MaxOutputTokens}
		if p.ResponseFormat != "prompt" {
			c.ResponseFormat = &openai.ChatCompletionResponseFormat{}
			if json.Unmarshal([]byte(formatJSON), c.ResponseFormat) != nil {
				return "", errors.New("输出协议配置无效")
			}
		}
		cm, err = openai.NewChatModel(ctx, c)
	}
	if err != nil {
		return "", errors.New("Eino 模型初始化失败")
	}
	msg, err := cm.Generate(ctx, messages)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("请求超时或取消")
		}
		format := "unknown"
		if recorded := transport.format.Load(); recorded != nil {
			format = recorded.(string)
		}
		return "", fmt.Errorf("upstream_failure http_status=%d body_format=%s error_type=%T", transport.status.Load(), format, err)
	}
	if msg == nil || msg.ResponseMeta == nil {
		return "", errors.New("模型返回缺少结束状态")
	}
	if msg.ResponseMeta.FinishReason != "stop" {
		reason := "unknown"
		switch msg.ResponseMeta.FinishReason {
		case "length", "content_filter", "tool_calls", "function_call":
			reason = msg.ResponseMeta.FinishReason
		}
		tokens := 0
		if msg.ResponseMeta.Usage != nil {
			tokens = msg.ResponseMeta.Usage.CompletionTokens
		}
		return "", fmt.Errorf("模型输出未完整结束 finish_reason=%s completion_tokens=%d", reason, tokens)
	}
	if err := validateOutput(msg.Content); err != nil {
		return "", err
	}
	return msg.Content, nil
}

func validateOutput(content string) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &fields) != nil {
		return fmt.Errorf("返回内容不是完整 JSON markdown_fence=%t", strings.HasPrefix(strings.TrimSpace(content), "```"))
	}
	if len(fields) != 2 {
		_, summary := fields["summary"]
		_, issues := fields["issues"]
		return fmt.Errorf("返回字段不符合测试 JSON 结构 field_count=%d summary_present=%t issues_present=%t", len(fields), summary, issues)
	}
	var summary string
	var issues []string
	if json.Unmarshal(fields["summary"], &summary) != nil || strings.TrimSpace(summary) == "" || bytes.Equal(bytes.TrimSpace(fields["issues"]), []byte("null")) || json.Unmarshal(fields["issues"], &issues) != nil {
		return errors.New("返回内容缺少有效 summary 或 issues 数组")
	}
	return nil
}
