package httpserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"shiftory-server/internal/auth"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
)

func TestCoreAPIWorkflow(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test jwt key: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	tokens := auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: tokens})
	if err != nil {
		t.Fatalf("create router: %v", err)
	}

	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")

	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{
		"name": "护理一组", "timezone": "Asia/Shanghai",
	})
	workspaceID := jsonUint(t, workspace, "data", "id")

	invitation := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, map[string]any{
		"email": "bob@example.com", "role": "MEMBER",
	})
	inviteToken := jsonString(t, invitation, "data", "token")
	apiRequest(t, router, http.MethodPost, "/api/v1/invitations/accept", bob.AccessToken, map[string]any{"token": inviteToken})

	// Keep the fixture's membership interval stable: the schedule dates below
	// are intentionally fixed and must fall after both members joined.
	if _, err := db.Exec(`UPDATE workspace_members SET joined_at = '2026-09-01 00:00:00' WHERE workspace_id = ?`, workspaceID); err != nil {
		t.Fatalf("set deterministic membership start: %v", err)
	}

	shift := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/shifts", workspaceID), alice.AccessToken, map[string]any{
		"name": "机动班", "code": "FLEX", "startTime": "08:30", "endTime": "17:30", "crossDay": false,
		"displayColor": "#22a06b", "aliases": []string{"早", "白班"},
	})
	shiftID := jsonUint(t, shift, "data", "id")

	apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d/2026-09-04", workspaceID, alice.UserID), alice.AccessToken, map[string]any{
		"status": "WORKING", "note": "正常班", "version": 0,
		"segments": []map[string]any{{"type": "SHIFT", "shiftId": shiftID}},
	})
	apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d/2026-09-04", workspaceID, bob.UserID), bob.AccessToken, map[string]any{
		"status": "REST", "note": "轮休", "version": 0, "segments": []any{},
	})

	calendar := apiRequest(t, router, http.MethodGet, fmt.Sprintf(
		"/api/v1/workspaces/%d/calendar?start=2026-09-04&end=2026-09-05&memberIds=%d,%d", workspaceID, alice.UserID, bob.UserID), alice.AccessToken, nil)
	days := jsonArray(t, calendar, "data", "days")
	if len(days) != 2 {
		t.Fatalf("expected two calendar days, got %d", len(days))
	}
	first := days[0].(map[string]any)
	if first["working"] != float64(1) || first["rest"] != float64(1) || first["missing"] != float64(0) || first["allRest"] != false {
		t.Fatalf("unexpected first calendar day: %+v", first)
	}
	second := days[1].(map[string]any)
	if second["missing"] != float64(2) {
		t.Fatalf("expected both members missing on second day: %+v", second)
	}

	response := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d?start=2026-09-04&end=2026-09-04", workspaceID, alice.UserID), bob.AccessToken, nil)
	items := jsonArray(t, response, "data", "items")
	if len(items) != 1 || items[0].(map[string]any)["note"] != "正常班" {
		t.Fatalf("members must see complete confirmed schedules: %+v", items)
	}
	apiRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/%d/members/%d", workspaceID, bob.UserID), alice.AccessToken, map[string]any{"role": "MEMBER", "status": "REMOVED"})
	if _, err := db.Exec(`UPDATE workspace_members SET left_at = '2026-09-04 00:00:00' WHERE workspace_id = ? AND user_id = ?`, workspaceID, bob.UserID); err != nil {
		t.Fatalf("set deterministic membership end: %v", err)
	}
	historical := apiRequest(t, router, http.MethodGet, fmt.Sprintf(
		"/api/v1/workspaces/%d/calendar?start=2026-09-04&end=2026-09-05&memberIds=%d", workspaceID, bob.UserID), alice.AccessToken, nil)
	historicalDays := jsonArray(t, historical, "data", "days")
	if len(historicalDays[0].(map[string]any)["members"].([]any)) != 1 || len(historicalDays[1].(map[string]any)["members"].([]any)) != 0 {
		t.Fatalf("removed member must be counted only inside the membership effective dates: %+v", historicalDays)
	}
}

func TestManagementAndPreferenceAPIWorkflow(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test jwt key: %v", err)
	}
	cfg, _ := config.Load()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)})
	if err != nil {
		t.Fatalf("create router: %v", err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")

	profile := apiRequest(t, router, http.MethodPatch, "/api/v1/auth/profile", alice.AccessToken, map[string]any{"displayName": "Alice Zhang", "avatarUrl": "https://example.com/alice.png"})
	if jsonString(t, profile, "data", "displayName") != "Alice Zhang" {
		t.Fatalf("profile update did not persist: %+v", profile)
	}
	apiRequest(t, router, http.MethodPut, "/api/v1/auth/password", alice.AccessToken, map[string]any{"currentPassword": "correct horse battery staple", "newPassword": "another correct horse battery staple"})

	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "护理一组", "timezone": "Asia/Shanghai"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	invitation := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, map[string]any{"username": "bob", "role": "MEMBER"})
	if jsonString(t, invitation, "data", "username") != "bob" { t.Fatalf("username invitation did not resolve: %+v", invitation) }
	apiRequest(t, router, http.MethodPost, "/api/v1/invitations/accept", bob.AccessToken, map[string]any{"token": jsonString(t, invitation, "data", "token")})

	apiRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/%d", workspaceID), alice.AccessToken, map[string]any{"name": "护理协同组", "timezone": "Asia/Shanghai"})
	apiRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/%d/members/%d", workspaceID, bob.UserID), alice.AccessToken, map[string]any{"role": "ADMIN", "status": "ACTIVE"})
	members := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/members", workspaceID), alice.AccessToken, nil)
	memberItems := jsonArray(t, members, "data", "items")
	if len(memberItems) != 2 {
		t.Fatalf("expected two members: %+v", members)
	}
	if _, exists := memberItems[0].(map[string]any)["scheduleCompleteness"]; !exists {
		t.Fatalf("member list must include current-month schedule completeness: %+v", memberItems[0])
	}

	preference := apiRequest(t, router, http.MethodPut, "/api/v1/preferences", alice.AccessToken, map[string]any{"currentWorkspaceId": workspaceID, "theme": "lilac"})
	if jsonString(t, preference, "data", "theme") != "lilac" {
		t.Fatalf("theme preference did not persist: %+v", preference)
	}

	shifts := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/shifts", workspaceID), alice.AccessToken, nil)
	defaultShifts := jsonArray(t, shifts, "data", "items")
	shiftID := uint64(defaultShifts[0].(map[string]any)["id"].(float64))
	apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/shifts/%d", workspaceID, shiftID), alice.AccessToken, map[string]any{
		"name": "标准早班", "code": "MORNING", "startTime": "08:30", "endTime": "16:30", "crossDay": false,
		"displayColor": "#16a34a", "enabled": true, "sortOrder": 1, "aliases": []string{"早", "早班"},
	})

	apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/schedules/batch", workspaceID), alice.AccessToken, map[string]any{
		"userId": bob.UserID, "dates": []string{"2026-09-04", "2026-09-05"}, "status": "REST", "note": "统一轮休", "segments": []any{},
	})
	history := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d/2026-09-04/history", workspaceID, bob.UserID), alice.AccessToken, nil)
	if len(jsonArray(t, history, "data", "items")) == 0 {
		t.Fatal("expected schedule history")
	}

	overview := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/overview?date=2026-09-04", workspaceID), bob.AccessToken, nil)
	if jsonAt(t, overview, "data", "rest") != float64(1) {
		t.Fatalf("unexpected overview: %+v", overview)
	}
	audits := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/audits", workspaceID), alice.AccessToken, nil)
	if len(jsonArray(t, audits, "data", "items")) == 0 {
		t.Fatal("expected audit entries")
	}
}

func TestSpreadsheetImportPreviewCommitAndRollback(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	cfg.UploadDir = t.TempDir()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)})
	if err != nil {
		t.Fatalf("create router: %v", err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "护理一组", "timezone": "Asia/Shanghai"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d/2026-09-04", workspaceID, alice.UserID), alice.AccessToken, map[string]any{
		"status": "REST", "note": "原排班", "version": 0, "segments": []any{},
	})

	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	values := [][]any{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-04", "工作", "早班", "", "", "否", "导入排班"},
		{"2026-09-05", "休息", "", "", "", "否", ""},
		{"2026-09-06", "工作", "尚未映射班", "", "", "否", "需人工核对"},
	}
	for row, columns := range values {
		for column, value := range columns {
			cell, _ := excelize.CoordinatesToCellName(column+1, row+1)
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}
	content, _ := workbook.WriteToBuffer()
	upload := multipartRequest(t, router, fmt.Sprintf("/api/v1/workspaces/%d/imports", workspaceID), alice.AccessToken, map[string]string{
		"targetUserId": fmt.Sprint(alice.UserID), "periodStart": "2026-09-04", "periodEnd": "2026-09-06",
	}, "file", "schedule.xlsx", content.Bytes())
	jobID := jsonUint(t, upload, "data", "id")
	if jsonString(t, upload, "data", "state") != "NEEDS_REVIEW" || jsonAt(t, upload, "data", "conflictCount") != float64(1) || jsonAt(t, upload, "data", "invalidCount") != float64(1) {
		t.Fatalf("unexpected import preview summary: %+v", upload)
	}

	preview := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d", workspaceID, jobID), alice.AccessToken, nil)
	items := jsonArray(t, preview, "data", "items")
	if len(items) != 3 || items[0].(map[string]any)["type"] != "CONFLICT" || items[1].(map[string]any)["type"] != "NEW" || items[2].(map[string]any)["type"] != "UNCERTAIN" {
		t.Fatalf("unexpected preview items: %+v", items)
	}
	if issues, ok := items[2].(map[string]any)["issues"].([]any); !ok || len(issues) != 1 {
		t.Fatalf("unmapped shift must explain the correction needed: %+v", items[2])
	}
	apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/decisions", workspaceID, jobID), alice.AccessToken, map[string]any{
		"decisions": []map[string]any{{"itemId": uint64(items[0].(map[string]any)["id"].(float64)), "decision": "USE_IMPORTED"}},
	})
	apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/commit", workspaceID, jobID), alice.AccessToken, map[string]any{})

	afterCommit := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d?start=2026-09-04&end=2026-09-05", workspaceID, alice.UserID), alice.AccessToken, nil)
	committed := jsonArray(t, afterCommit, "data", "items")
	if committed[0].(map[string]any)["status"] != "WORKING" || committed[1].(map[string]any)["status"] != "REST" {
		t.Fatalf("import was not committed: %+v", committed)
	}

	apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/rollback", workspaceID, jobID), alice.AccessToken, map[string]any{})
	afterRollback := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d?start=2026-09-04&end=2026-09-05", workspaceID, alice.UserID), alice.AccessToken, nil)
	rolledBack := jsonArray(t, afterRollback, "data", "items")
	if len(rolledBack) != 1 || rolledBack[0].(map[string]any)["status"] != "REST" || rolledBack[0].(map[string]any)["note"] != "原排班" {
		t.Fatalf("rollback did not restore prior state: %+v", rolledBack)
	}

	list := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/imports", workspaceID), alice.AccessToken, nil)
	if len(jsonArray(t, list, "data", "items")) != 1 {
		t.Fatalf("expected import history: %+v", list)
	}
}

func TestImageImportAccessDownloadAndCancellation(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	cfg.UploadDir = t.TempDir()
	cfg.AIEnabled = true
	wakeups := 0
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour), ImportWakeup: func() { wakeups++ }})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")
	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "护理一组", "timezone": "Asia/Shanghai"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	invitation := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, map[string]any{"email": "bob@example.com", "role": "MEMBER"})
	apiRequest(t, router, http.MethodPost, "/api/v1/invitations/accept", bob.AccessToken, map[string]any{"token": jsonString(t, invitation, "data", "token")})
	pngData, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	upload := multipartRequest(t, router, fmt.Sprintf("/api/v1/workspaces/%d/imports/image", workspaceID), alice.AccessToken, map[string]string{
		"targetUserId": fmt.Sprint(alice.UserID), "periodStart": "2026-09-01", "periodEnd": "2026-09-30", "instructions": "蓝色表示休息",
	}, "file", "schedule.png", pngData)
	jobID := jsonUint(t, upload, "data", "id")
	if jsonString(t, upload, "data", "state") != "PENDING" {
		t.Fatalf("expected pending image import: %+v", upload)
	}
	if wakeups != 1 {
		t.Fatalf("expected one worker wakeup after commit, got %d", wakeups)
	}
	bobImports := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/imports", workspaceID), bob.AccessToken, nil)
	if len(jsonArray(t, bobImports, "data", "items")) != 0 {
		t.Fatalf("member must only see imports they uploaded or target themselves: %+v", bobImports)
	}
	if code, _ := rawAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d", workspaceID, jobID), bob.AccessToken, nil); code != http.StatusForbidden {
		t.Fatalf("expected import details to be private, got %d", code)
	}
	if code, body := rawAPIRequest(router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/file", workspaceID, jobID), alice.AccessToken, nil); code != http.StatusOK || !bytes.Equal(body, pngData) {
		t.Fatalf("unexpected original file response: code=%d body=%x", code, body)
	}
	cancelled := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/cancel", workspaceID, jobID), alice.AccessToken, map[string]any{})
	if jsonString(t, cancelled, "data", "state") != "CANCELLED" {
		t.Fatalf("unexpected cancelled job: %+v", cancelled)
	}
}

func TestUncertainImportItemCanBeCorrectedBeforeCommit(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	cfg.UploadDir = t.TempDir()
	cfg.AIEnabled = true
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "护理一组", "timezone": "Asia/Shanghai"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	pngData, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	upload := multipartRequest(t, router, fmt.Sprintf("/api/v1/workspaces/%d/imports/image", workspaceID), alice.AccessToken, map[string]string{
		"targetUserId": fmt.Sprint(alice.UserID), "periodStart": "2026-09-04", "periodEnd": "2026-09-04",
	}, "file", "schedule.png", pngData)
	jobID := jsonUint(t, upload, "data", "id")
	if _, err := db.Exec(`UPDATE import_jobs SET state = 'NEEDS_REVIEW', item_count = 1, invalid_count = 1 WHERE id = ?`, jobID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO import_items (import_job_id, work_date, item_type, issues, sort_order) VALUES (?, '2026-09-04', 'UNCERTAIN', JSON_ARRAY('无法识别班次'), 0)`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	itemID, _ := result.LastInsertId()

	corrected := apiRequest(t, router, http.MethodPut, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/items/%d", workspaceID, jobID, itemID), alice.AccessToken, map[string]any{
		"status": "WORKING", "note": "人工核对", "segments": []map[string]any{{"type": "TIME_RANGE", "startTime": "08:15", "endTime": "17:45", "crossDay": false}},
	})
	if jsonString(t, corrected, "data", "type") != "NEW" {
		t.Fatalf("corrected item should become committable: %+v", corrected)
	}
	apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/imports/%d/commit", workspaceID, jobID), alice.AccessToken, map[string]any{})
	schedules := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/schedules/%d?start=2026-09-04&end=2026-09-04", workspaceID, alice.UserID), alice.AccessToken, nil)
	items := jsonArray(t, schedules, "data", "items")
	if len(items) != 1 || items[0].(map[string]any)["note"] != "人工核对" {
		t.Fatalf("corrected import item was not committed: %+v", schedules)
	}
}

func TestOwnerTransferInvitationRevocationAndWorkspaceDeletion(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "alice", "alice@example.com")
	bob := registerAndLogin(t, router, "bob", "bob@example.com")
	workspace := apiRequest(t, router, http.MethodPost, "/api/v1/workspaces", alice.AccessToken, map[string]any{"name": "待移交工作区", "timezone": "Asia/Shanghai"})
	workspaceID := jsonUint(t, workspace, "data", "id")
	pending := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, map[string]any{"email": "nobody@example.com", "role": "MEMBER"})
	pendingID := jsonUint(t, pending, "data", "id")
	listed := apiRequest(t, router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, nil)
	if len(jsonArray(t, listed, "data", "items")) != 1 {
		t.Fatalf("expected pending invitation: %+v", listed)
	}
	apiRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/%d/invitations/%d", workspaceID, pendingID), alice.AccessToken, nil)

	invite := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/invitations", workspaceID), alice.AccessToken, map[string]any{"email": "bob@example.com", "role": "MEMBER"})
	apiRequest(t, router, http.MethodPost, "/api/v1/invitations/accept", bob.AccessToken, map[string]any{"token": jsonString(t, invite, "data", "token")})
	transferred := apiRequest(t, router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/transfer", workspaceID), alice.AccessToken, map[string]any{"newOwnerUserId": bob.UserID})
	if jsonAt(t, transferred, "data", "ownerUserId") != float64(bob.UserID) {
		t.Fatalf("unexpected transfer response: %+v", transferred)
	}
	apiRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/%d", workspaceID), bob.AccessToken, map[string]any{"confirmationName": "待移交工作区"})
	workspaces := apiRequest(t, router, http.MethodGet, "/api/v1/workspaces", bob.AccessToken, nil)
	if len(jsonArray(t, workspaces, "data", "items")) != 0 {
		t.Fatalf("workspace was not deleted: %+v", workspaces)
	}
}

func TestRefreshJWTRequiresCSRFAndRotatesCookie(t *testing.T) {
	db := openCleanTestDatabase(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	router, _ := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(privateKey, publicKey, "test-key", cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute, 7*24*time.Hour)})
	apiRequest(t, router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"username": "alice", "email": "alice@example.com", "displayName": "Alice", "password": "correct horse battery staple"})
	payload, _ := json.Marshal(map[string]any{"login": "alice@example.com", "password": "correct horse battery staple"})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(payload))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", cfg.PublicOrigin)
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login failed: %s", loginResponse.Body.String())
	}
	loginCookies := loginResponse.Result().Cookies()
	var refreshCookie, csrfCookie *http.Cookie
	for _, cookie := range loginCookies {
		switch cookie.Name {
		case "shiftory_refresh":
			refreshCookie = cookie
		case "shiftory_csrf":
			csrfCookie = cookie
		}
	}
	if refreshCookie == nil || csrfCookie == nil || csrfCookie.HttpOnly {
		t.Fatalf("expected HttpOnly refresh plus readable CSRF cookie: %+v", loginCookies)
	}
	requestRefresh := func(withCSRF bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", cfg.PublicOrigin)
		request.AddCookie(refreshCookie)
		request.AddCookie(csrfCookie)
		if withCSRF {
			request.Header.Set("X-CSRF-Token", csrfCookie.Value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := requestRefresh(false); response.Code != http.StatusForbidden {
		t.Fatalf("refresh without CSRF returned %d: %s", response.Code, response.Body.String())
	}
	response := requestRefresh(true)
	if response.Code != http.StatusOK {
		t.Fatalf("refresh with CSRF failed: %d %s", response.Code, response.Body.String())
	}
	rotated := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "shiftory_refresh" && cookie.Value != refreshCookie.Value {
			rotated = true
		}
	}
	if !rotated {
		t.Fatal("expected refresh token cookie rotation")
	}
	var securityAuditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE workspace_id IS NULL AND action IN ('LOGIN_SUCCEEDED', 'REFRESH_SUCCEEDED')`).Scan(&securityAuditCount); err != nil {
		t.Fatal(err)
	}
	if securityAuditCount != 2 {
		t.Fatalf("expected login and refresh security audits, got %d", securityAuditCount)
	}
}

type loginResult struct {
	UserID      uint64
	AccessToken string
}

func registerAndLogin(t *testing.T, handler http.Handler, username, email string) loginResult {
	t.Helper()
	registered := apiRequest(t, handler, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"username": username, "email": email, "displayName": strings.ToUpper(username[:1]) + username[1:], "password": "correct horse battery staple",
	})
	userID := jsonUint(t, registered, "data", "id")
	login := apiRequest(t, handler, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"login": email, "password": "correct horse battery staple"})
	return loginResult{UserID: userID, AccessToken: jsonString(t, login, "data", "accessToken")}
}

func apiRequest(t *testing.T, handler http.Handler, method, path, accessToken string, body any) map[string]any {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:5173")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, response.Code, response.Body.String())
	}
	result := map[string]any{}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %s %s: %v: %s", method, path, err, response.Body.String())
	}
	return result
}

func rawAPIRequest(handler http.Handler, method, path, accessToken string, body []byte) (int, []byte) {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Origin", "http://localhost:5173")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func multipartRequest(t *testing.T, handler http.Handler, path, accessToken string, fields map[string]string, fileField, fileName string, content []byte) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write multipart field: %v", err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Origin", "http://localhost:5173")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code < 200 || response.Code >= 300 {
		t.Fatalf("POST %s returned %d: %s", path, response.Code, response.Body.String())
	}
	result := map[string]any{}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode multipart response: %v", err)
	}
	return result
}

func jsonUint(t *testing.T, value map[string]any, path ...string) uint64 {
	t.Helper()
	current := jsonAt(t, value, path...)
	number, ok := current.(float64)
	if !ok {
		t.Fatalf("expected number at %v, got %#v", path, current)
	}
	return uint64(number)
}

func jsonString(t *testing.T, value map[string]any, path ...string) string {
	t.Helper()
	current := jsonAt(t, value, path...)
	text, ok := current.(string)
	if !ok {
		t.Fatalf("expected string at %v, got %#v", path, current)
	}
	return text
}

func jsonArray(t *testing.T, value map[string]any, path ...string) []any {
	t.Helper()
	current := jsonAt(t, value, path...)
	items, ok := current.([]any)
	if !ok {
		t.Fatalf("expected array at %v, got %#v", path, current)
	}
	return items
}

func jsonAt(t *testing.T, value map[string]any, path ...string) any {
	t.Helper()
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("expected object before %q in %v, got %#v", part, path, current)
		}
		current = object[part]
	}
	return current
}

func openCleanTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	admin, err := database.Open(context.Background(), "root:123456@tcp(127.0.0.1:3306)/mysql?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("open MySQL for test database creation: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE IF NOT EXISTS shiftory_test_httpserver CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`); err != nil {
		_ = admin.Close()
		t.Fatalf("create isolated test database: %v", err)
	}
	_ = admin.Close()
	dsn := "root:123456@tcp(127.0.0.1:3306)/shiftory_test_httpserver?charset=utf8mb4&parseTime=true&loc=UTC"
	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	tables := []string{
		"user_preferences", "audit_logs", "auth_refresh_tokens", "import_items", "schedule_revisions",
		"schedule_segments", "schedule_days", "import_files", "import_jobs", "shift_aliases", "shifts",
		"workspace_invitations", "workspace_members", "workspaces", "users",
	}
	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	for _, table := range tables {
		if _, err := db.Exec("TRUNCATE TABLE " + table); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS=1"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	return db
}
