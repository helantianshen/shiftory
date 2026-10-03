package ai

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"os"
	"shiftory-server/internal/importer/imageai"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/schedule"
	"sync/atomic"
	"testing"
	"time"
)

func TestEinoRoutingRejectsInvalidAndFencedOutputThenStops(t *testing.T) {
	addr := os.Getenv("SHIFTORY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated Redis keys via SHIFTORY_TEST_REDIS_ADDR")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer rc.Close()
	if err := rc.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	var first, second, third atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		w.WriteHeader(503)
		w.Write([]byte(`{"error":{"message":"private-secret"}}`))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		var request map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&request)
		if _, ok := request["temperature"]; ok {
			t.Error("unexpected temperature")
		}
		if _, ok := request["top_p"]; ok {
			t.Error("unexpected top_p")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "```json\n{\"period\":{\"start\":\"2026-10-01\",\"end\":\"2026-10-01\"},\"entries\":[{\"date\":\"2026-10-01\",\"status\":\"REST\",\"segments\":[],\"issues\":[],\"uncertain\":false}],\"issues\":[]}\n```"}, "finish_reason": "stop"}}})
	}))
	defer good.Close()
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { third.Add(1) }))
	defer never.Close()
	cfg := config.AIConfig{RoundTimeout: 5 * time.Second, MaxResponseBytes: 2 << 20, MaxOutputTokens: 4096, FailureThreshold: 1, Cooldown: time.Minute}
	for i, u := range []string{bad.URL, good.URL, never.URL} {
		cfg.Providers = append(cfg.Providers, config.Provider{ID: []string{"bad", "good", "never"}[i], Supplier: "relay", Kind: "openai", Enabled: true, Order: i + 1, BaseURL: u + "/v1", APIKey: "fake-key", Model: "fixture", ImageEnabled: true, TextEnabled: true, ResponseFormat: "json_object", RequestTimeout: "1s", MaxConcurrency: 1})
	}
	r, err := New(context.Background(), cfg, rc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		keys, _ := rc.Keys(context.Background(), "shiftory:ai:"+r.fingerprint+":*").Result()
		if len(keys) > 0 {
			rc.Del(context.Background(), keys...)
		}
	}()
	in := imageai.Request{Image: []byte{1}, ImageFormat: "png", Start: schedule.MustDate("2026-10-01"), End: schedule.MustDate("2026-10-01")}
	draft, err := r.Recognize(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Load() != 1 || second.Load() != 1 || third.Load() != 0 || len(draft.Entries) != 1 || draft.RawResponse == "" {
		t.Fatal("routing or hidden retry failed", first.Load(), second.Load(), third.Load())
	}
	if _, err = r.Recognize(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if first.Load() != 1 {
		t.Fatal("shared cooldown ignored")
	}
}

func TestLiveFormalProviderContracts(t *testing.T) {
	if os.Getenv("SHIFTORY_LIVE_AI") != "true" {
		t.Skip("requires explicit live AI opt-in")
	}
	cfg, err := config.Load("--env", "development", "--config", "../../../config/ai.development.yaml")
	if err != nil {
		t.Fatal("invalid live config")
	}
	rc := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address(), DB: 15})
	defer rc.Close()
	png, err := os.ReadFile("../../../tools/ai-smoke/testdata/schedule.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range cfg.AI.Providers {
		for _, kind := range []string{"text", "image"} {
			t.Run(provider.ID+"_"+kind, func(t *testing.T) {
				selected := cfg.AI
				selected.Providers = []config.Provider{provider}
				r, err := New(context.Background(), selected, rc)
				if err != nil {
					t.Fatal(err)
				}
				in := imageai.Request{Start: schedule.MustDate("2026-10-01"), End: schedule.MustDate("2026-10-03"), Timezone: "Asia/Shanghai", FixedNow: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ShiftMappings: map[string]string{}, OnCall: func(ctx context.Context, c imageai.Call) error {
					if !c.FinishedAt.IsZero() {
						t.Logf("provider=%s kind=%s code=%s finish=%s tokens=%d", provider.ID, kind, c.Code, c.FinishReason, c.OutputTokens)
						if c.Code == "INVALID_AI_OUTPUT" && kind == "text" {
							if data, e := imageai.StripJSONFence(c.Raw); e == nil {
								_, e = imageai.DecodeRules(data, schedule.MustDate("2026-10-01"), schedule.MustDate("2026-10-03"))
								t.Logf("rule_validation=%v", e)
							}
						}
					}
					return nil
				}}
				if kind == "text" {
					in.Description = "每周一至周五09:00至18:00工作，周末休息。10月3日改为22:00至次日06:00工作。不额外应用节假日或调休规则。"
				} else {
					in.Image = png
					in.ImageFormat = "png"
				}
				draft, err := r.Recognize(context.Background(), in)
				if err != nil {
					t.Fatal(err)
				}
				if len(draft.Entries) != 3 || draft.Entries[2].Status != "WORKING" || len(draft.Entries[2].Segments) != 1 || !draft.Entries[2].Segments[0].CrossDay {
					t.Fatal("formal contract semantics failed")
				}
			})
		}
	}
}

func TestExplicitUpstreamCodeClassification(t *testing.T) {
	for _, test := range []struct {
		body, code string
		status     int
		retry      bool
		retryAfter string
	}{
		{`{"error":{"code":"insufficient_quota"}}`, "QUOTA_EXHAUSTED", 429, false, ""},
		{`{"error":{"code":"quota_exceeded"}}`, "QUOTA_EXHAUSTED", 429, true, "60"},
		{`{"error":{"code":"rate_limit_exceeded"}}`, "UPSTREAM_FAILURE", 429, true, ""},
		{`{"error":{"code":"content_policy_violation"}}`, "CONTENT_REFUSED", 400, false, ""},
		{`{"error":{"code":"unknown"}}`, "AI_CONFIGURATION_ERROR", 400, false, ""},
	} {
		t.Run(test.code+test.retryAfter, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", test.retryAfter)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			r := &Recognizer{cfg: config.AIConfig{MaxResponseBytes: 4096, MaxOutputTokens: 100}}
			_, _, err := r.generate(context.Background(), config.Provider{ID: "fixture", Kind: "openai", APIKey: "fake", BaseURL: server.URL + "/v1", Model: "fixture", ResponseFormat: "json_object", RequestTimeout: "1s"}, imageai.Request{Description: "休息", Start: schedule.MustDate("2026-10-01"), End: schedule.MustDate("2026-10-01")}, &Call{})
			if err == nil || err.Code != test.code || err.Retryable != test.retry {
				t.Fatal("classification mismatch", err)
			}
		})
	}
}

func TestSharedProviderSlotsHalfOpenAndCredentialRotation(t *testing.T) {
	addr := os.Getenv("SHIFTORY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires Redis fixture")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer rc.Close()
	cfg := config.AIConfig{RoundTimeout: time.Second, FailureThreshold: 1, Cooldown: time.Second, Providers: []config.Provider{{ID: "shared", APIKey: "test-key", Enabled: true, RequestTimeout: "1s", MaxConcurrency: 1}}}
	a, e := New(context.Background(), cfg, rc)
	if e != nil {
		t.Fatal(e)
	}
	b, e := New(context.Background(), cfg, rc)
	if e != nil {
		t.Fatal(e)
	}
	prefix := "shiftory:ai:" + a.fingerprint + ":shared"
	defer func() {
		keys, _ := rc.Keys(context.Background(), prefix+":*").Result()
		if len(keys) > 0 {
			rc.Del(context.Background(), keys...)
		}
	}()
	p := cfg.Providers[0]
	ok, release, e := a.acquire(context.Background(), p)
	if e != nil || !ok {
		t.Fatal(e)
	}
	ok, _, e = b.acquire(context.Background(), p)
	if e != nil || ok {
		t.Fatal("shared limit ignored", e)
	}
	release()
	if e = a.recordHealth(context.Background(), p, &ProcessingError{Code: "UPSTREAM_FAILURE", Retryable: true}); e != nil {
		t.Fatal(e)
	}
	ok, _, e = b.acquire(context.Background(), p)
	if ok || e == nil {
		t.Fatal("cooldown ignored")
	}
	if e = rc.Del(context.Background(), prefix+":cool").Err(); e != nil {
		t.Fatal(e)
	}
	p.MaxConcurrency = 2
	ok, release, e = a.acquire(context.Background(), p)
	if e != nil || !ok {
		t.Fatal("half-open unavailable", e)
	}
	ok, _, e = b.acquire(context.Background(), p)
	if e != nil || ok {
		t.Fatal("multiple half-open probes", e)
	}
	release()
	if e = a.recordHealth(context.Background(), p, &ProcessingError{Code: "AI_CONFIGURATION_ERROR"}); e != nil {
		t.Fatal(e)
	}
	ok, _, e = b.acquire(context.Background(), p)
	if ok || e == nil {
		t.Fatal("permanent config error ignored")
	}
	cfg.Providers[0].APIKey = "rotated-key"
	c, e := New(context.Background(), cfg, rc)
	if e != nil {
		t.Fatal(e)
	}
	if c.fingerprint == a.fingerprint {
		t.Fatal("credential rotation ignored")
	}
	ok, release, e = c.acquire(context.Background(), cfg.Providers[0])
	if e != nil || !ok {
		t.Fatal("rotated key still blocked", e)
	}
	release()
	keys, _ := rc.Keys(context.Background(), "shiftory:ai:"+c.fingerprint+":*").Result()
	if len(keys) > 0 {
		rc.Del(context.Background(), keys...)
	}
}
