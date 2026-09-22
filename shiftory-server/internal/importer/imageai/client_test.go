package imageai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"shiftory-server/internal/schedule"
)

type capturingModel struct {
	request *agentmodel.Request
	content string
	err     error
}

// TestOpenAICompatibleClientUsesConfiguredTimeout 验证兼容客户端遵守配置的请求超时
func TestOpenAICompatibleClientUsesConfiguredTimeout(t *testing.T) {
	client, err := NewOpenAICompatibleWithTimeout("vision-test", "test-key", "", 45*time.Second)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if client.timeout != 45*time.Second {
		t.Fatalf("unexpected timeout %s", client.timeout)
	}
}

// Info 返回测试模型的固定元信息
func (m *capturingModel) Info() agentmodel.Info { return agentmodel.Info{Name: "fake-vision"} }

// GenerateContent 记录模型请求并返回预设响应，避免真实模型调用
func (m *capturingModel) GenerateContent(_ context.Context, request *agentmodel.Request) (<-chan *agentmodel.Response, error) {
	m.request = request
	if m.err != nil {
		return nil, m.err
	}
	responses := make(chan *agentmodel.Response, 1)
	responses <- &agentmodel.Response{Done: true, Choices: []agentmodel.Choice{{Message: agentmodel.NewAssistantMessage(m.content)}}}
	close(responses)
	return responses, nil
}

// TestClientSendsImageWithStrictStructuredOutput 验证模型请求包含图像内容和严格结构化输出配置
func TestClientSendsImageWithStrictStructuredOutput(t *testing.T) {
	model := &capturingModel{content: `{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[{"date":"2026-09-01","status":"REST","note":"","segments":[],"uncertain":false,"issues":[]}]}`}
	client := NewClient(model)
	draft, err := client.Recognize(context.Background(), Request{
		Image: []byte{1, 2, 3}, ImageFormat: "png", Start: schedule.MustDate("2026-09-01"), End: schedule.MustDate("2026-09-30"),
		Instructions: "蓝色单元格表示休息", ShiftMappings: map[string]string{"早": "MORNING"},
	})
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if len(draft.Entries) != 1 || draft.Entries[0].Status != "REST" {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	if model.request == nil || model.request.StructuredOutput == nil || !model.request.StructuredOutput.JSONSchema.Strict {
		t.Fatal("expected strict structured output")
	}
	if model.request.Temperature != nil || model.request.TopP != nil {
		t.Fatal("temperature and top_p must be omitted for provider compatibility")
	}
	if len(model.request.Messages) != 2 || len(model.request.Messages[1].ContentParts) != 2 || model.request.Messages[1].ContentParts[1].Image == nil {
		t.Fatalf("expected text and inline image: %+v", model.request.Messages)
	}
	if !strings.Contains(model.request.Messages[0].Content, `只能使用 entries 和 segments`) {
		t.Fatal("expected exact output field guidance in system prompt")
	}
}

// TestClientPreservesJPEGMediaSubtype 验证 JPEG 图片在请求中保持正确媒体子类型
func TestClientPreservesJPEGMediaSubtype(t *testing.T) {
	model := &capturingModel{content: `{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[],"issues":[]}`}
	client := NewClient(model)
	if _, err := client.Recognize(context.Background(), Request{
		Image: []byte{1, 2, 3}, ImageFormat: "jpeg", Start: schedule.MustDate("2026-09-01"), End: schedule.MustDate("2026-09-30"),
	}); err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if got := model.request.Messages[1].ContentParts[1].Image.Format; got != "jpeg" {
		t.Fatalf("expected jpeg media subtype, got %q", got)
	}
}

// TestOpenAICompatibleDeepSeekVisionKeepsImageAndUsesJSONObject 检查 DeepSeek 视觉请求保留图片且响应格式为 json_object
func TestOpenAICompatibleDeepSeekVisionKeepsImageAndUsesJSONObject(t *testing.T) {
	var payload struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"period\":{\"start\":\"2026-09-01\",\"end\":\"2026-09-30\"},\"entries\":[],\"issues\":[]}"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleWithTimeout("deepseek-v4-flash-vision-exp", "test-key", server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if _, err := client.Recognize(context.Background(), Request{
		Image: []byte{1, 2, 3}, ImageFormat: "jpeg", Start: schedule.MustDate("2026-09-01"), End: schedule.MustDate("2026-09-30"),
	}); err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if payload.ResponseFormat.Type != "json_object" {
		t.Fatalf("expected json_object response format, got %q", payload.ResponseFormat.Type)
	}
	if len(payload.Messages) != 2 || !strings.Contains(string(payload.Messages[1].Content), `"image_url"`) {
		t.Fatalf("expected inline image content, got %s", payload.Messages[1].Content)
	}
	if !strings.Contains(string(payload.Messages[1].Content), "data:image/jpeg;base64,") {
		t.Fatalf("expected image/jpeg data URL, got %s", payload.Messages[1].Content)
	}
}
