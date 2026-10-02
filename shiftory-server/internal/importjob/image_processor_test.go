package importjob

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"testing"
	"time"

	"shiftory-server/internal/importer/imageai"
	"shiftory-server/internal/platform/storage"
	"shiftory-server/internal/schedule"
)

type memoryStore struct{ content []byte }

// Put 满足测试存储接口，写入操作不产生副作用
func (s memoryStore) Put(context.Context, string, io.Reader) error { return nil }

// Open 返回预设图片字节的独立读取器，由调用方关闭
func (s memoryStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.content)), nil
}

// Delete 满足测试存储接口，删除操作不产生副作用
func (s memoryStore) Delete(context.Context, string) error { return nil }

var _ storage.Store = memoryStore{}

type fakeRecognizer struct{ draft imageai.Draft }

// Recognize 返回预设草稿供图片处理测试使用，不调用外部模型
func (r fakeRecognizer) Recognize(context.Context, imageai.Request) (imageai.Draft, error) {
	return r.draft, nil
}

// TestImageProcessorCreatesReviewItemsWithoutWritingSchedules 验证图片处理补齐缺失日期并只生成预览、不写正式排班
func TestImageProcessorCreatesReviewItemsWithoutWritingSchedules(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PENDING", time.Now().UTC())
	var workspaceID, targetUserID uint64
	if err := db.QueryRow(`SELECT workspace_id, target_user_id FROM import_jobs LIMIT 1`).Scan(&workspaceID, &targetUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shifts (workspace_id, name, code, start_time, end_time, cross_day, display_color, created_by) VALUES (?, '早班', 'MORNING', '08:00:00', '16:00:00', FALSE, '#22a06b', ?)`, workspaceID, targetUserID); err != nil {
		t.Fatal(err)
	}
	pngData, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	processor := NewImageProcessor(db, memoryStore{content: pngData}, fakeRecognizer{draft: imageai.Draft{
		Period: imageai.Period{Start: "2026-09-01", End: "2026-09-30"},
		Entries: []imageai.Entry{
			{Date: "2026-09-01", Status: "WORKING", Segments: []imageai.Segment{{Type: "SHIFT", OriginalLabel: "早", MappedShiftCode: "MORNING", StartTime: "08:00", EndTime: "16:00"}}, Issues: []imageai.Issue{}},
			{Date: "2026-09-02", Status: "REST", Uncertain: true, Issues: []imageai.Issue{{Field: "status", Message: "模糊"}}, Segments: []imageai.Segment{}},
		},
	}}, "fake-vision")
	result, err := processor.Process(context.Background(), Job{ID: 1, WorkspaceID: workspaceID, TargetUserID: targetUserID, PeriodStart: schedule.MustDate("2026-09-01"), PeriodEnd: schedule.MustDate("2026-09-30"), StorageKey: "imports/test.png", MediaType: "image/png"})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if result.ItemCount != 30 || len(result.Items) != 30 || result.Items[0].Type != "NEW" || result.Items[1].Type != "UNCERTAIN" || result.Items[2].Type != "MISSING" {
		t.Fatalf("unexpected result: count=%d items=%+v", result.ItemCount, result.Items[:3])
	}
	var scheduleCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM schedule_days`).Scan(&scheduleCount)
	if scheduleCount != 0 {
		t.Fatal("image processor must not write formal schedules")
	}
}

// TestImagePreviewFindsGlobalSchedule 验证图片预览能够识别其他工作区创建的已有排班
func TestImagePreviewFindsGlobalSchedule(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PENDING", time.Now().UTC())
	var workspaceID, userID uint64
	if err := db.QueryRow("SELECT workspace_id, target_user_id FROM import_jobs LIMIT 1").Scan(&workspaceID, &userID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec("INSERT INTO workspaces (name,timezone,owner_user_id,created_by) VALUES ('Other','UTC',?,?)", userID, userID)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := result.LastInsertId()
	if _, err := db.Exec("INSERT INTO schedule_days (workspace_id,user_id,work_date,status,source_type,note,version,created_by) VALUES (?,?,'2026-09-01','REST','MANUAL','global',3,?)", otherID, userID, userID); err != nil {
		t.Fatal(err)
	}
	processor := NewImageProcessor(db, memoryStore{}, fakeRecognizer{}, "test")
	snapshot, id, version, found, err := processor.loadExisting(context.Background(), workspaceID, userID, schedule.MustDate("2026-09-01"))
	if err != nil || !found || id == 0 || version != 3 || snapshot.Note != "global" {
		t.Fatalf("global preview lookup failed: found=%v version=%d err=%v", found, version, err)
	}
}
