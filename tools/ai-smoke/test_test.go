package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func sampleConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := loadConfig("../../config/ai.development.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestConfigurationAndPriority(t *testing.T) {
	cfg := sampleConfig(t)
	if len(cfg.AI.Providers) != 3 || cfg.AI.Providers[0].ID != "doubao" || cfg.AI.Providers[2].ID != "deepseek" {
		t.Fatal("priority order changed")
	}
	if _, err := candidates(cfg, "auto", true); err == nil {
		t.Fatal("empty credentials should prevent network calls")
	}
	data, err := os.ReadFile("../../config/ai.development.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{strings.Replace(string(data), "order: 20", "order: 10", 1), strings.Replace(string(data), "kind: ark", "kind: typo", 1), strings.Replace(string(data), "supplier: relay", "supplier: stepfun", 1), string(data) + "\nunknown: secret\n", string(data) + "\n---\nredis: {}\n"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	for i := range cfg.AI.Providers {
		cfg.AI.Providers[i].APIKey = "fake-key"
		cfg.AI.Providers[i].BaseURL = "http://127.0.0.1"
		cfg.AI.Providers[i].Model = "fake-model"
	}
	cfg.AI.Providers[0].ImageEnabled = false
	providers, err := candidates(cfg, "auto", true)
	if err != nil || len(providers) != 2 || providers[0].ID != "relay" {
		t.Fatal("image capability filtering failed")
	}
}

func TestEinoAdapters(t *testing.T) {
	for _, kind := range []string{"ark", "openai"} {
		for _, format := range []string{"prompt", "json_object", "json_schema"} {
			t.Run(kind+"/"+format, func(t *testing.T) {
				cfg := sampleConfig(t)
				var count atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					count.Add(1)
					if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fake-key" {
						t.Error("unexpected endpoint or credentials")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["model"] != "fake-model" {
						t.Error("model not forwarded")
					}
					messages := body["messages"].([]any)
					parts, ok := messages[1].(map[string]any)["content"].([]any)
					if !ok || len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
						t.Error("multimodal image missing")
					}
					if format == "prompt" {
						if _, exists := body["response_format"]; exists {
							t.Error("prompt must omit response_format")
						}
					} else {
						rf := body["response_format"].(map[string]any)
						if rf["type"] != format {
							t.Error("response format mismatch")
						}
						if format == "json_schema" && rf["json_schema"].(map[string]any)["schema"] == nil {
							t.Error("schema missing")
						}
					}
					if kind == "ark" && body["thinking"].(map[string]any)["type"] != "disabled" {
						t.Error("thinking configuration missing")
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":"mock","object":"chat.completion","created":1,"model":"fake-model","choices":[{"index":0,"message":{"role":"assistant","content":"{\"summary\":\"排班规则\",\"issues\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				}))
				defer server.Close()
				p := cfg.AI.Providers[0]
				p.Kind, p.BaseURL, p.APIKey, p.Model, p.ResponseFormat = kind, server.URL, "fake-key", "fake-model", format
				encoded := "aW1hZ2U="
				message := &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "识别排班"}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &encoded, MIMEType: "image/png"}}}}}
				if _, err := call(context.Background(), p, cfg, []*schema.Message{schema.SystemMessage("返回 JSON"), message}); err != nil {
					t.Fatal(err)
				}
				if count.Load() != 1 {
					t.Fatal("unexpected SDK retries")
				}
			})
		}
	}
}

func TestFailuresAndCancellation(t *testing.T) {
	cfg := sampleConfig(t)
	for _, kind := range []string{"ark", "openai"} {
		t.Run(kind, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":{"message":"fake-secret","type":"server_error","code":"unavailable"}}`)
			}))
			defer server.Close()
			p := cfg.AI.Providers[0]
			p.Kind, p.BaseURL, p.APIKey, p.Model = kind, server.URL, "fake-secret", "fake-model"
			_, err := call(context.Background(), p, cfg, []*schema.Message{schema.UserMessage("test")})
			if err == nil || strings.Contains(err.Error(), "fake-secret") || !strings.Contains(err.Error(), "503") || count.Load() != 1 {
				t.Fatalf("unsafe error or unexpected retries: %v, count=%d", err, count.Load())
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := call(ctx, p, cfg, []*schema.Message{schema.UserMessage("test")}); err == nil {
				t.Fatal("cancellation ignored")
			}
		})
	}
	for _, value := range []string{`null`, `{"summary":"ok","issues":null}`, `{"summary":"ok","issues":[],"extra":true}`, "```json\n{}\n```"} {
		if validateOutput(value) == nil {
			t.Fatal("invalid model output accepted")
		}
	}
	if validateOutput(`{"summary":"ok","issues":[]}`) != nil {
		t.Fatal("valid output rejected")
	}
}

func TestTruncatedOutputDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"mock","object":"chat.completion","created":1,"model":"fake-model","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":1,"completion_tokens":4096,"total_tokens":4097}}`)
	}))
	defer server.Close()
	cfg := sampleConfig(t)
	for _, kind := range []string{"ark", "openai"} {
		p := cfg.AI.Providers[0]
		p.Kind, p.BaseURL, p.APIKey, p.Model = kind, server.URL, "fake-key", "fake-model"
		_, err := call(context.Background(), p, cfg, []*schema.Message{schema.UserMessage("test")})
		if err == nil || !strings.Contains(err.Error(), "finish_reason=length") || !strings.Contains(err.Error(), "completion_tokens=4096") {
			t.Fatalf("%s truncation diagnostic missing: %v", kind, err)
		}
	}
}

func TestHTMLResponseIsNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!DOCTYPE html><html>fake-secret</html>")
	}))
	defer server.Close()
	cfg := sampleConfig(t)
	p := cfg.AI.Providers[1]
	p.BaseURL, p.APIKey, p.Model = server.URL, "fake-secret", "fake-model"
	_, err := call(context.Background(), p, cfg, []*schema.Message{schema.UserMessage("test")})
	if err == nil || !strings.Contains(err.Error(), "http_status=200") || !strings.Contains(err.Error(), "body_format=html_or_xml") || strings.Contains(err.Error(), "fake-secret") {
		t.Fatalf("HTML response not safely rejected: %v", err)
	}
}
