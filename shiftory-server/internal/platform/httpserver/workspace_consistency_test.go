package httpserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"shiftory-server/internal/auth"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/schedule"
)

// TestWorkspaceConsistency 验证跨工作区读取边界、邀请权限与解散后的个人排班保留
func TestWorkspaceConsistency(t *testing.T) {
	db := openCleanTestDatabase(t)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UploadDir = t.TempDir()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(private, public, "test", cfg.JWTIssuer, cfg.JWTAudience, time.Hour, 24*time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")
	carol := registerAndLogin(t, router, "carol", "carol@example.com")
	create := func(user loginResult, name string) uint64 {
		return jsonUint(t, apiRequest(t, router, "POST", "/api/v1/workspaces", user.AccessToken, map[string]any{"name": name, "timezone": "UTC"}), "data", "id")
	}
	a, b, c := create(alice, "A"), create(bob, "B"), create(carol, "C")
	path := func(id uint64, suffix string) string { return fmt.Sprintf("/api/v1/workspaces/%d%s", id, suffix) }
	invite := func(id uint64, actor loginResult, email, role string) uint64 {
		return jsonUint(t, apiRequest(t, router, "POST", path(id, "/invitations"), actor.AccessToken, map[string]any{"email": email, "role": role}), "data", "id")
	}
	accept := func(user loginResult, id uint64) {
		apiRequest(t, router, "POST", "/api/v1/invitations/accept", user.AccessToken, map[string]any{"invitationId": id})
	}
	status := func(method, url string, actor loginResult, input any, expected int) {
		body, _ := json.Marshal(input)
		code, response := rawAPIRequest(router, method, url, actor.AccessToken, body)
		if code != expected {
			t.Fatalf("%s %s: want %d, got %d: %s", method, url, expected, code, response)
		}
	}
	accept(bob, invite(a, alice, "bob@example.com", "ADMIN"))
	// 管理员不能通过邀请提权，重复邀请不能覆盖所有者角色
	status("POST", path(a, "/invitations"), bob, map[string]any{"email": "carol@example.com", "role": "ADMIN"}, http.StatusForbidden)
	duplicate := invite(a, alice, "alice@example.com", "MEMBER")
	status("POST", "/api/v1/invitations/accept", alice, map[string]any{"invitationId": duplicate}, http.StatusConflict)
	var role string
	if err := db.QueryRow("SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?", a, alice.UserID).Scan(&role); err != nil || role != "OWNER" {
		t.Fatal("owner role changed", err)
	}
	// 班次主体不变时仍可修改别名
	shiftPayload := map[string]any{"name": "早班X", "code": "EARLYX", "startTime": "08:00", "endTime": "16:00", "crossDay": false, "displayColor": "#22a06b", "aliases": []string{"旧别名"}, "enabled": true, "sortOrder": 0}
	shiftID := jsonUint(t, apiRequest(t, router, "POST", path(a, "/shifts"), alice.AccessToken, shiftPayload), "data", "id")
	shiftPayload["aliases"] = []string{"新别名"}
	apiRequest(t, router, "PUT", path(a, fmt.Sprintf("/shifts/%d", shiftID)), alice.AccessToken, shiftPayload)
	var aliases int
	if err := db.QueryRow("SELECT COUNT(*) FROM shift_aliases WHERE shift_id = ? AND alias = '新别名'", shiftID).Scan(&aliases); err != nil || aliases != 1 {
		t.Fatal("alias-only update failed", err)
	}
	date := time.Now().UTC().Format("2006-01-02")
	suffix := fmt.Sprintf("/schedules/%d", bob.UserID)
	apiRequest(t, router, "PUT", path(a, suffix+"/"+date), bob.AccessToken, map[string]any{"status": "WORKING", "note": "shared", "version": 0, "segments": []any{map[string]any{"type": "SHIFT", "shiftId": shiftID}}})
	rangeQuery := "?start=" + date + "&end=" + date
	status("GET", path(c, suffix+rangeQuery), carol, nil, http.StatusForbidden)
	status("GET", path(c, suffix+"/"+date+"/history"), carol, nil, http.StatusForbidden)
	overview := apiRequest(t, router, "GET", path(b, "/overview?date="+date), bob.AccessToken, nil)
	if jsonAt(t, overview, "data", "monthCompleteness").(float64) <= 0 || jsonString(t, overview, "data", "nextWorkingDate") != date {
		t.Fatal("global overview incorrect", overview)
	}
	members := jsonArray(t, apiRequest(t, router, "GET", path(b, "/members"), bob.AccessToken, nil), "data", "items")
	if members[0].(map[string]any)["scheduleCompleteness"].(float64) <= 0 {
		t.Fatal("global completeness missing")
	}
	// 降级后的上传者可以查看资料，但不能提交或撤销替他人创建的任务
	result, err := db.Exec("INSERT INTO import_jobs (workspace_id,upload_user_id,target_user_id,import_type,state,period_start,period_end,source_filename,idempotency_key) VALUES (?,?,?,'XLSX','NEEDS_REVIEW',?,?,'test.xlsx','scope-test')", a, bob.UserID, alice.UserID, date, date)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := result.LastInsertId()
	apiRequest(t, router, "PATCH", path(a, fmt.Sprintf("/members/%d", bob.UserID)), alice.AccessToken, map[string]any{"role": "MEMBER", "status": "ACTIVE"})
	status("POST", path(a, fmt.Sprintf("/imports/%d/commit", jobID)), bob, map[string]any{}, http.StatusForbidden)
	if _, err := db.Exec("UPDATE import_jobs SET state = 'COMPLETED' WHERE id = ?", jobID); err != nil {
		t.Fatal(err)
	}
	status("POST", path(a, fmt.Sprintf("/imports/%d/rollback", jobID)), bob, map[string]any{}, http.StatusForbidden)
	// 我的列表不能混入他人任务，管理员团队列表仍可查看全部任务
	mine := apiRequest(t, router, "GET", path(a, "/imports?scope=mine"), alice.AccessToken, nil)
	all := apiRequest(t, router, "GET", path(a, "/imports?scope=all"), alice.AccessToken, nil)
	if len(jsonArray(t, mine, "data", "items")) != 0 || len(jsonArray(t, all, "data", "items")) != 1 {
		t.Fatal("import scopes differ from request")
	}
	// 目标离开工作区后，管理员也不能通过旧任务继续修改其全局排班
	result, err = db.Exec("INSERT INTO import_jobs (workspace_id,upload_user_id,target_user_id,import_type,state,period_start,period_end,source_filename,idempotency_key) VALUES (?,?,?,'XLSX','NEEDS_REVIEW',?,?,'target.xlsx','removed-target')", a, alice.UserID, bob.UserID, date, date)
	if err != nil {
		t.Fatal(err)
	}
	targetJobID, _ := result.LastInsertId()
	apiRequest(t, router, "PATCH", path(a, fmt.Sprintf("/members/%d", bob.UserID)), alice.AccessToken, map[string]any{"role": "MEMBER", "status": "REMOVED"})
	status("POST", path(a, fmt.Sprintf("/imports/%d/commit", targetJobID)), alice, map[string]any{}, http.StatusForbidden)
	if _, err := db.Exec("UPDATE schedule_days SET source_import_id = ?, source_type = 'XLSX' WHERE user_id = ?", targetJobID, bob.UserID); err != nil {
		t.Fatal(err)
	}
	apiRequest(t, router, "DELETE", path(a, ""), alice.AccessToken, map[string]any{"confirmationName": "A"})
	saved := jsonArray(t, apiRequest(t, router, "GET", path(b, suffix+rangeQuery), bob.AccessToken, nil), "data", "items")
	if len(saved) != 1 || saved[0].(map[string]any)["note"] != "shared" || saved[0].(map[string]any)["sourceImportId"] != nil {
		t.Fatal("deleting workspace lost global schedule")
	}
	segments := saved[0].(map[string]any)["segments"].([]any)
	if segments[0].(map[string]any)["shiftName"] != "早班X" {
		t.Fatal("deleted shift lost its display snapshot")
	}
	history := apiRequest(t, router, "GET", path(b, suffix+"/"+date+"/history"), bob.AccessToken, nil)
	if len(jsonArray(t, history, "data", "items")) != 1 {
		t.Fatal("deleting workspace lost revision history")
	}
	// 原班次已删除时，仍允许只修改备注并保留服务端快照
	preservedID := segments[0].(map[string]any)["id"]
	apiRequest(t, router, "PUT", path(b, suffix+"/"+date), bob.AccessToken, map[string]any{"status": "WORKING", "note": "edited after deletion", "version": 1, "segments": []any{map[string]any{"type": "SHIFT", "existingSegmentId": preservedID}}})
	status("PUT", path(b, suffix+"/2026-11-01"), bob, map[string]any{"status": "WORKING", "version": 0, "segments": []any{map[string]any{"type": "SHIFT", "existingSegmentId": preservedID}}}, http.StatusBadRequest)

	// 撤销其他工作区导入时，旧快照中的来源外键可以失效，业务内容仍可恢复
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	existing, found, err := queryScheduleTx(context.Background(), tx, b, bob.UserID, schedule.MustDate(date), true)
	if err != nil || !found {
		t.Fatal("missing schedule for restoration", err)
	}
	restored := existing
	restored.Segments = append([]schedule.Segment(nil), existing.Segments...)
	restored.Segments[0].ShiftID = &shiftID
	deletedImportID := uint64(targetJobID)
	restored.SourceImportID = &deletedImportID
	restored.SourceType = schedule.SourceXLSX
	savedDay, err := writeScheduleTx(context.Background(), tx, restored, existing, true)
	if err != nil || savedDay.SourceImportID != nil || savedDay.Segments[0].ShiftID != nil || savedDay.Segments[0].ShiftName != "早班X" {
		t.Fatal("failed to restore a snapshot whose source was deleted", err)
	}

}

// TestPersonalScheduleScope 验证无工作区的个人排班读写、历史、批量和账号隔离
func TestPersonalScheduleScope(t *testing.T) {
	db := openCleanTestDatabase(t)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UploadDir = t.TempDir()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(private, public, "test", cfg.JWTIssuer, cfg.JWTAudience, time.Hour, 24*time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "personal-alice", "personal-alice@example.com")
	bob := registerAndLogin(t, router, "personal-bob", "personal-bob@example.com")
	saved := apiRequest(t, router, "PUT", "/api/v1/me/schedules/2026-10-01", alice.AccessToken, map[string]any{"status": "WORKING", "version": 0, "segments": []map[string]any{{"type": "TIME_RANGE", "startTime": "08:30", "endTime": "17:30", "crossDay": false}}})
	if jsonUint(t, saved, "data", "version") != 1 {
		t.Fatal("personal schedule not saved")
	}
	list := apiRequest(t, router, "GET", "/api/v1/me/schedules?start=2026-10-01&end=2026-10-31", alice.AccessToken, nil)
	if len(list["data"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("personal schedule missing")
	}
	history := apiRequest(t, router, "GET", "/api/v1/me/schedules/2026-10-01/history", alice.AccessToken, nil)
	if len(history["data"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("personal history missing")
	}
	other := apiRequest(t, router, "GET", "/api/v1/me/schedules?start=2026-10-01&end=2026-10-31&userId=1", bob.AccessToken, nil)
	if len(other["data"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatal("personal schedules leaked across accounts")
	}
	apiRequest(t, router, "POST", "/api/v1/me/schedules/batch", alice.AccessToken, map[string]any{"dates": []string{"2026-10-02", "2026-10-03"}, "status": "REST", "segments": []any{}})
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schedule_days WHERE workspace_id IS NULL").Scan(&count); err != nil || count != 3 {
		t.Fatalf("personal schedules tied to workspace: %d %v", count, err)
	}
	status := func(method, path string, actor loginResult, input any, expected int) {
		body, _ := json.Marshal(input)
		code, response := rawAPIRequest(router, method, path, actor.AccessToken, body)
		if code != expected {
			t.Fatalf("%s %s: expected %d, got %d: %s", method, path, expected, code, response)
		}
	}
	status("POST", "/api/v1/me/schedules/batch", bob, map[string]any{"userId": jsonUint(t, saved, "data", "userId"), "dates": []string{"2026-10-02"}, "status": "REST"}, http.StatusForbidden)
	workspace := apiRequest(t, router, "POST", "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "source", "timezone": "UTC"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	var shiftID uint64
	if err := db.QueryRow("SELECT id FROM shifts WHERE workspace_id=? LIMIT 1", workspaceID).Scan(&shiftID); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"status": "WORKING", "version": 0, "shiftWorkspaceId": workspaceID, "segments": []map[string]any{{"type": "SHIFT", "shiftId": shiftID}}}
	status("PUT", "/api/v1/me/schedules/2026-10-04", bob, input, http.StatusForbidden)
	apiRequest(t, router, "PUT", "/api/v1/me/schedules/2026-10-04", alice.AccessToken, input)
	apiRequest(t, router, "DELETE", fmt.Sprintf("/api/v1/workspaces/%d", workspaceID), alice.AccessToken, map[string]any{"confirmationName": "source"})
	preserved := apiRequest(t, router, "GET", "/api/v1/me/schedules?start=2026-10-04&end=2026-10-04", alice.AccessToken, nil)
	day := preserved["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	segment := day["segments"].([]any)[0].(map[string]any)
	apiRequest(t, router, "PUT", "/api/v1/me/schedules/2026-10-04", alice.AccessToken, map[string]any{"status": "WORKING", "version": day["version"], "note": "retained", "segments": []map[string]any{{"type": "SHIFT", "existingSegmentId": segment["id"]}}})
	apiRequest(t, router, "GET", "/api/v1/me/schedules/2026-10-04/history", alice.AccessToken, nil)
}
