package imageai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	"shiftory-server/internal/schedule"
)

// Request 提供单成员图片、明确日期范围及用户识别提示
type Request struct {
	Image         []byte
	ImageFormat   string
	Start         schedule.Date
	End           schedule.Date
	Instructions  string
	ShiftMappings map[string]string
}

// Client 包装模型实例、调用超时和诊断日志
type Client struct {
	model   agentmodel.Model
	timeout time.Duration
	logger  *slog.Logger
}

// NewClient 以默认日志器包装已有模型实例
func NewClient(model agentmodel.Model) *Client {
	return NewClientWithLogger(model, slog.Default())
}

// NewClientWithLogger 包装模型并设置默认请求超时与日志器
func NewClientWithLogger(model agentmodel.Model, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{model: model, timeout: 90 * time.Second, logger: logger}
}

// NewOpenAICompatible 使用默认超时创建兼容 OpenAI 协议的视觉客户端
func NewOpenAICompatible(modelName, apiKey, baseURL string) (*Client, error) {
	return NewOpenAICompatibleWithTimeout(modelName, apiKey, baseURL, 90*time.Second)
}

// NewOpenAICompatibleWithTimeout 使用指定超时和默认日志器创建视觉客户端
func NewOpenAICompatibleWithTimeout(modelName, apiKey, baseURL string, timeout time.Duration) (*Client, error) {
	return NewOpenAICompatibleWithTimeoutAndLogger(modelName, apiKey, baseURL, timeout, slog.Default())
}

// NewOpenAICompatibleWithTimeoutAndLogger 校验模型配置并构造带超时与日志器的视觉客户端
func NewOpenAICompatibleWithTimeoutAndLogger(modelName, apiKey, baseURL string, timeout time.Duration, logger *slog.Logger) (*Client, error) {
	if strings.TrimSpace(modelName) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("AI model and API key are required")
	}
	if timeout <= 0 {
		return nil, errors.New("AI request timeout must be positive")
	}
	// DeepSeek SDK 变体默认按纯文本组装消息，视觉模型需要保留图像内容块
	options := []openai.Option{openai.WithAPIKey(apiKey), openai.WithTextOnlyMessageContent(false)}
	// 自定义网关地址无法供 SDK 推断 DeepSeek 协议变体，模型前缀提供明确的兼容信息
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "deepseek-") {
		options = append(options, openai.WithVariant(openai.VariantDeepSeek))
	}
	if strings.TrimSpace(baseURL) != "" {
		options = append(options, openai.WithBaseURL(strings.TrimRight(baseURL, "/")))
	}
	client := NewClientWithLogger(openai.New(modelName, options...), logger)
	client.timeout = timeout
	return client, nil
}

// Recognize 发送图片及识别约束，汇集模型响应并返回经校验的排班草稿
func (c *Client) Recognize(ctx context.Context, input Request) (Draft, error) {
	if c == nil || c.model == nil {
		return Draft{}, errors.New("image AI model is not configured")
	}
	if len(input.Image) == 0 {
		return Draft{}, errors.New("image is empty")
	}
	if _, err := schedule.ParseDate(input.Start.String()); err != nil {
		return Draft{}, err
	}
	if _, err := schedule.ParseDate(input.End.String()); err != nil || input.Start.String() > input.End.String() {
		return Draft{}, errors.New("requested image date range is invalid")
	}
	format := strings.ToLower(strings.TrimPrefix(input.ImageFormat, "."))
	if format != "png" && format != "jpg" && format != "jpeg" && format != "webp" && format != "gif" {
		return Draft{}, errors.New("unsupported image format")
	}

	// 将模型调用限制在配置超时内，系统提示和用户周期共同约束输出范围
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	system := agentmodel.NewSystemMessage(`你是 Shiftory 排班截图识别器。只提取图中明确可见的信息，不猜测目标成员，不执行任何修改。请只输出合法 JSON，不要 Markdown 代码围栏。必须严格仿照下面的 JSON 结构和字段类型返回；不要返回 null，不要增加字段，不要使用小写枚举、custom、work、off 或 24:00。日期必须是 YYYY-MM-DD；无法可靠判断的条目必须 uncertain=true 并给出字段级 issues。顶层只能使用 period、entries 和 issues；每个 entries 项只能使用 date、status、note、segments、uncertain 和 issues；每个 segments 项只能使用 type、originalLabel、mappedShiftCode、startTime、endTime 和 crossDay。status 只能是 WORKING 或 REST；segment type 只能是 SHIFT 或 TIME_RANGE；时间必须是 HH:mm，午夜请使用 00:00 并设置 crossDay=true。只能使用 entries 和 segments 字段，不要改成 schedules 或 shifts。

合法返回示例：
{
  "period": {"start": "2026-09-01", "end": "2026-09-30"},
  "entries": [
    {
      "date": "2026-09-07",
      "status": "WORKING",
      "note": "",
      "segments": [
        {"type": "SHIFT", "originalLabel": "早班", "mappedShiftCode": "MORNING", "startTime": "08:00", "endTime": "16:00", "crossDay": false}
      ],
      "uncertain": false,
      "issues": []
    },
    {
      "date": "2026-09-08",
      "status": "REST",
      "note": "休",
      "segments": [],
      "uncertain": false,
      "issues": []
    },
    {
      "date": "2026-09-09",
      "status": "WORKING",
      "note": "无法确认班次",
      "segments": [
        {"type": "TIME_RANGE", "originalLabel": "10:00-24:00", "mappedShiftCode": "", "startTime": "10:00", "endTime": "00:00", "crossDay": true}
      ],
      "uncertain": true,
      "issues": [{"field": "segments[0]", "message": "无法确认班次代码"}]
    }
  ],
  "issues": []
}`)
	// 用户说明和班次映射作为文本元数据，与原图片内容块一同发送
	text := buildUserPrompt(input)
	user := agentmodel.Message{Role: agentmodel.RoleUser, ContentParts: []agentmodel.ContentPart{{Type: agentmodel.ContentTypeText, Text: &text}}}
	user.AddImageData(input.Image, "high", format)
	request := agentmodel.NewRequest([]agentmodel.Message{system, user}, agentmodel.WithStructuredOutputJSON(new(Draft), true, "Extract one member's schedule into a bounded, reviewable Shiftory draft."))
	// 部分视觉模型网关会拒绝 temperature 和 top_p 字段，请求不得携带这两个字段
	request.GenerationConfig = agentmodel.GenerationConfig{Stream: false}
	responses, err := c.model.GenerateContent(ctx, request)
	if err != nil {
		logger := c.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Debug("image AI provider call error", "error", err)
		return Draft{}, fmt.Errorf("call image AI model: %w", err)
	}
	// 完整消息替换已有内容，增量消息继续拼接，最终统一进行草稿解码
	var content strings.Builder
	responseCount := 0
	for response := range responses {
		if response == nil {
			continue
		}
		responseCount++
		logger := c.logger
		if logger == nil {
			logger = slog.Default()
		}
		// 原始响应先写入 debug 日志，错误响应和空响应也需要保留诊断信息
		if rawResponse, marshalErr := json.Marshal(response); marshalErr == nil {
			logger.Debug("image AI provider response", "content", string(rawResponse))
		} else {
			logger.Debug("image AI provider response", "content", fmt.Sprintf("%+v", response), "marshal_error", marshalErr)
		}
		if response.Error != nil {
			return Draft{}, fmt.Errorf("image AI response: %w", response.Error)
		}
		for _, choice := range response.Choices {
			if choice.Message.Content != "" {
				content.Reset()
				content.WriteString(choice.Message.Content)
			} else if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
			}
		}
	}
	if strings.TrimSpace(content.String()) == "" {
		logger := c.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Debug("image AI returned empty content", "response_count", responseCount)
		return Draft{}, errors.New("image AI returned no structured content")
	}
	// 完整响应可能较大，只在 debug 级别输出以便定位 Schema 不一致
	rawContent := content.String()
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Debug("image AI raw response", "content", rawContent)
	return DecodeDraft(bytes.NewBufferString(rawContent), input.Start, input.End)
}

// buildUserPrompt 将用户指定周期、识别说明及班次映射提示组织为模型输入
func buildUserPrompt(input Request) string {
	aliases := make([]string, 0, len(input.ShiftMappings))
	for alias, code := range input.ShiftMappings {
		aliases = append(aliases, fmt.Sprintf("%s => %s", alias, code))
	}
	sort.Strings(aliases)
	metadata, _ := json.Marshal(map[string]any{
		"requestedPeriod": map[string]string{"start": input.Start.String(), "end": input.End.String()},
		"shiftMappings":   aliases,
		"instructions":    strings.TrimSpace(input.Instructions),
	})
	return "请识别这张单成员排班截图。以下边界由用户明确提供，不得扩大日期范围：\n" + string(metadata)
}
