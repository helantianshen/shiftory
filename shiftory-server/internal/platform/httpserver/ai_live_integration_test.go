package httpserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"os"
	"shiftory-server/internal/ai"
	"shiftory-server/internal/auth"
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/storage"
	"testing"
	"time"
)

func TestLiveAIImportWorkflow(t *testing.T) {
	if os.Getenv("SHIFTORY_LIVE_AI") != "true" {
		t.Skip("live provider calls require SHIFTORY_LIVE_AI=true")
	}
	db := openCleanTestDatabase(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg, e := config.Load("--env", "development", "--config", "../../../../config/ai.development.yaml")
	if e != nil {
		t.Fatal("live AI config invalid")
	}
	cfg.UploadDir = t.TempDir()
	cfg.Redis.DB = 15
	cfg.Tasks.Queue = fmt.Sprintf("shiftory_live_%d", time.Now().UnixNano())
	cfg.Tasks.OutboxPollInterval = 100 * time.Millisecond
	rc := redis.NewClient(importjob.RedisOptions(cfg.Redis))
	defer rc.Close()
	if e = rc.Ping(context.Background()).Err(); e != nil {
		t.Fatal("live Redis unavailable")
	}
	store, e := storage.NewLocal(cfg.UploadDir)
	if e != nil {
		t.Fatal(e)
	}
	recognizer, e := ai.New(context.Background(), cfg.AI, rc)
	if e != nil {
		t.Fatal(e)
	}
	processor := importjob.NewImageProcessorWithLogger(db, store, recognizer, "routed", slog.Default())
	q := importjob.NewQueue(db, cfg, processor, rc, slog.Default())
	if e = q.Start(); e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	root, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); q.Dispatch(root) }()
	defer func() { stop(); <-done }()
	router, e := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(priv, pub, "fixture", cfg.JWTIssuer, cfg.JWTAudience, time.Hour, time.Hour), Store: store, ImportCancel: q.Cancel, QueueReady: q.Ready})
	if e != nil {
		t.Fatal(e)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	workspace := apiRequest(t, router, "POST", "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "AI acceptance", "timezone": "Asia/Shanghai"})
	wid := jsonUint(t, workspace, "data", "id")
	base := fmt.Sprintf("/api/v1/workspaces/%d/imports", wid)
	png, e := os.ReadFile("../../../../tools/ai-smoke/testdata/schedule.png")
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"text", "image"} {
		t.Run(kind, func(t *testing.T) {
			var response map[string]any
			if kind == "text" {
				response = apiRequest(t, router, "POST", base+"/text", alice.AccessToken, map[string]any{"targetUserId": alice.UserID, "periodStart": "2026-10-01", "periodEnd": "2026-10-03", "description": "每周一到周五09:00至18:00工作，周六周日休息。10月3日改为22:00至次日06:00工作，不额外应用节假日或调休规则。"})
			} else {
				response = multipartRequest(t, router, base+"/image", alice.AccessToken, map[string]string{"targetUserId": fmt.Sprint(alice.UserID), "periodStart": "2026-10-01", "periodEnd": "2026-10-03"}, "file", "fixture.png", png)
			}
			id := jsonUint(t, response, "data", "id")
			detail := fmt.Sprintf("%s/%d", base, id)
			deadline := time.Now().Add(4 * time.Minute)
			var state string
			for {
				if e = db.QueryRow(`SELECT state FROM import_jobs WHERE id=$1`, id).Scan(&state); e != nil {
					t.Fatal(e)
				}
				if state == "NEEDS_REVIEW" {
					break
				}
				if state == "FAILED" || time.Now().After(deadline) {
					var code string
					_ = db.QueryRow(`SELECT COALESCE(error_code,'') FROM import_jobs WHERE id=$1`, id).Scan(&code)
					t.Fatal("live processing failed", state, code)
				}
				time.Sleep(200 * time.Millisecond)
			}
			preview := apiRequest(t, router, "GET", detail, alice.AccessToken, nil)
			items := jsonArray(t, preview, "data", "items")
			if len(items) != 3 {
				t.Fatal("incorrect review range")
			}
			if kind == "text" {
				first := items[0].(map[string]any)["draft"].(map[string]any)
				if first["status"] != "WORKING" {
					t.Fatal("weekday rule not expanded")
				}
			}
			last := items[2].(map[string]any)["draft"].(map[string]any)
			segments := last["segments"].([]any)
			if last["status"] != "WORKING" || len(segments) != 1 || segments[0].(map[string]any)["crossDay"] != true {
				t.Fatal("cross-day exception missing")
			}
			apiRequest(t, router, "POST", detail+"/commit", alice.AccessToken, map[string]any{"expectedReviewVersion": jsonUint(t, preview, "data", "reviewVersion")})
			apiRequest(t, router, "POST", detail+"/rollback", alice.AccessToken, map[string]any{})
			var count int
			_ = db.QueryRow(`SELECT COUNT(*) FROM schedule_days`).Scan(&count)
			if count != 0 {
				t.Fatal("rollback did not restore empty baseline")
			}
			t.Log("live queue, review, commit and rollback passed", kind)
		})
	}
}
