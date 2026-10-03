package httpserver

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"shiftory-server/internal/auth"
	"shiftory-server/internal/platform/config"
	"testing"
	"time"
)

func TestTextImportIdempotencyReviewVersionAndPrivacy(t *testing.T) {
	db := openCleanTestDatabase(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	cfg.AIEnabled = true
	cfg.UploadDir = t.TempDir()
	router, e := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(priv, pub, "fixture", cfg.JWTIssuer, cfg.JWTAudience, time.Minute, time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")
	w := apiRequest(t, router, "POST", "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "AI fixture", "timezone": "Asia/Shanghai"})
	wid := jsonUint(t, w, "data", "id")
	path := fmt.Sprintf("/api/v1/workspaces/%d/imports/text", wid)
	input := map[string]any{"targetUserId": alice.UserID, "periodStart": "2026-10-01", "periodEnd": "2026-10-02", "description": "每周一至周五工作，周末休息"}
	create := func() (int, map[string]any) {
		data, _ := json.Marshal(input)
		req := httptest.NewRequest("POST", path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+alice.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "fixed-request")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		var result map[string]any
		_ = json.Unmarshal(out.Body.Bytes(), &result)
		return out.Code, result
	}
	code, first := create()
	if code != 202 {
		t.Fatal(code, first)
	}
	id := jsonUint(t, first, "data", "id")
	code, second := create()
	if code != 202 || jsonUint(t, second, "data", "id") != id {
		t.Fatal("same input not idempotent")
	}
	input["description"] = "不同描述"
	if code, _ = create(); code != 409 {
		t.Fatal("different input accepted", code)
	}
	detail := fmt.Sprintf("/api/v1/workspaces/%d/imports/%d", wid, id)
	if code, _ := rawAPIRequest(router, "GET", detail, bob.AccessToken, nil); code != http.StatusForbidden {
		t.Fatal("private description exposed")
	}
	if code, _ := rawAPIRequest(router, "GET", detail+"/file", alice.AccessToken, nil); code != 404 {
		t.Fatal("text import has file")
	}
	_, e = db.Exec(`UPDATE import_jobs SET state='NEEDS_REVIEW' WHERE id=$1`, id)
	if e != nil {
		t.Fatal(e)
	}
	var item int64
	e = db.QueryRow(`INSERT INTO import_items(import_job_id,work_date,item_type,draft_snapshot)VALUES($1,'2026-10-01','NEW','{"status":"REST","note":"","segments":[]}') RETURNING id`, id).Scan(&item)
	if e != nil {
		t.Fatal(e)
	}

	apiRequest(t, router, "PUT", detail+"/decisions", alice.AccessToken, map[string]any{"expectedReviewVersion": 1, "decisions": []any{map[string]any{"itemId": item, "decision": "SKIP"}}})
	if code, _ := rawAPIRequest(router, "POST", detail+"/commit", alice.AccessToken, []byte(`{"expectedReviewVersion":1}`)); code != 409 {
		t.Fatal("stale version committed")
	}
	committed := apiRequest(t, router, "POST", detail+"/commit", alice.AccessToken, map[string]any{"expectedReviewVersion": 2})
	if jsonUint(t, committed, "data", "writeCount") != 0 {
		t.Fatal("skip wrote schedule")
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM schedule_days`).Scan(&count)
	if count != 0 {
		t.Fatal("AI creation wrote business data")
	}
}

func TestDissolveWorkspaceRemovesAIExecutionRecords(t *testing.T) {
	db := openCleanTestDatabase(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	cfg.UploadDir = t.TempDir()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(priv, pub, "fixture", cfg.JWTIssuer, cfg.JWTAudience, time.Hour, time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	w := apiRequest(t, router, "POST", "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "AI cleanup", "timezone": "UTC"})
	wid := jsonUint(t, w, "data", "id")
	var job int64
	err = db.QueryRow(`INSERT INTO import_jobs(workspace_id,upload_user_id,target_user_id,import_type,state,source_filename,idempotency_key)VALUES($1,$2,$3,'TEXT_AI','NEEDS_REVIEW','fixture','fixture') RETURNING id`, wid, alice.UserID, alice.UserID).Scan(&job)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(`INSERT INTO import_outbox(job_id,generation)VALUES($1,1)`, job); err != nil {
		t.Fatal(err)
	}
	var attempt int64
	err = db.QueryRow(`INSERT INTO import_attempts(job_id,generation,round,state)VALUES($1,1,1,'SUCCESS') RETURNING id`, job).Scan(&attempt)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(`INSERT INTO import_provider_calls(attempt_id,provider_id,model,sequence,raw_text,started_at)VALUES($1,'fixture','fixture',1,'private fixture',statement_timestamp())`, attempt); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, router, "DELETE", fmt.Sprintf("/api/v1/workspaces/%d", wid), alice.AccessToken, map[string]any{"confirmationName": "AI cleanup"})
	for _, table := range []string{"import_provider_calls", "import_attempts", "import_outbox", "import_jobs"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("AI records retained", table, n, err)
		}
	}
}
