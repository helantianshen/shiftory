package imageai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	"shiftory-server/internal/schedule"
)

type Request struct {
	Image         []byte
	ImageFormat   string
	Start         schedule.Date
	End           schedule.Date
	Instructions  string
	ShiftMappings map[string]string
}

type Client struct {
	model   agentmodel.Model
	timeout time.Duration
}

func NewClient(model agentmodel.Model) *Client {
	return &Client{model: model, timeout: 90 * time.Second}
}

func NewOpenAICompatible(modelName, apiKey, baseURL string) (*Client, error) {
	return NewOpenAICompatibleWithTimeout(modelName, apiKey, baseURL, 90*time.Second)
}

func NewOpenAICompatibleWithTimeout(modelName, apiKey, baseURL string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(modelName) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("AI model and API key are required")
	}
	if timeout <= 0 {
		return nil, errors.New("AI request timeout must be positive")
	}
	options := []openai.Option{openai.WithAPIKey(apiKey)}
	if strings.TrimSpace(baseURL) != "" {
		options = append(options, openai.WithBaseURL(strings.TrimRight(baseURL, "/")))
	}
	client := NewClient(openai.New(modelName, options...))
	client.timeout = timeout
	return client, nil
}

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
	if format == "jpeg" {
		format = "jpg"
	}
	if format != "png" && format != "jpg" && format != "webp" && format != "gif" {
		return Draft{}, errors.New("unsupported image format")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	system := agentmodel.NewSystemMessage(`你是 Shiftory 排班截图识别器。只提取图中明确可见的信息，不猜测目标成员，不执行任何修改。日期必须是 YYYY-MM-DD；无法可靠判断的条目必须 uncertain=true 并给出字段级 issues。结果必须严格符合输出 JSON Schema。`)
	text := buildUserPrompt(input)
	user := agentmodel.Message{Role: agentmodel.RoleUser, ContentParts: []agentmodel.ContentPart{{Type: agentmodel.ContentTypeText, Text: &text}}}
	user.AddImageData(input.Image, "high", format)
	zero := 0.0
	request := agentmodel.NewRequest([]agentmodel.Message{system, user}, agentmodel.WithStructuredOutputJSON(new(Draft), true, "Extract one member's schedule into a bounded, reviewable Shiftory draft."))
	request.GenerationConfig = agentmodel.GenerationConfig{Temperature: &zero, Stream: false}
	responses, err := c.model.GenerateContent(ctx, request)
	if err != nil {
		return Draft{}, fmt.Errorf("call image AI model: %w", err)
	}
	var content strings.Builder
	for response := range responses {
		if response == nil {
			continue
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
		return Draft{}, errors.New("image AI returned no structured content")
	}
	return DecodeDraft(bytes.NewBufferString(content.String()), input.Start, input.End)
}

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
