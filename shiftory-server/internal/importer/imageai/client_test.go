package imageai

import (
	"context"
	"testing"

	agentmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"shiftory-server/internal/schedule"
)

type capturingModel struct {
	request *agentmodel.Request
	content string
	err     error
}

func (m *capturingModel) Info() agentmodel.Info { return agentmodel.Info{Name: "fake-vision"} }

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
	if model.request.Temperature == nil || *model.request.Temperature != 0 {
		t.Fatal("expected deterministic zero temperature")
	}
	if len(model.request.Messages) != 2 || len(model.request.Messages[1].ContentParts) != 2 || model.request.Messages[1].ContentParts[1].Image == nil {
		t.Fatalf("expected text and inline image: %+v", model.request.Messages)
	}
}
