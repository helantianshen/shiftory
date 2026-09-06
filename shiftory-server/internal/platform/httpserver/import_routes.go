package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	_ "golang.org/x/image/webp"

	"shiftory-server/internal/importer"
	"shiftory-server/internal/schedule"
)

const maxImportBytes = int64(10 << 20)

type storedSegment struct {
	Type          schedule.SegmentType `json:"type"`
	ShiftID       *uint64              `json:"shiftId,omitempty"`
	ShiftName     string               `json:"shiftName,omitempty"`
	ShiftCode     string               `json:"shiftCode,omitempty"`
	StartTime     string               `json:"startTime,omitempty"`
	EndTime       string               `json:"endTime,omitempty"`
	CrossDay      bool                 `json:"crossDay"`
	DisplayColor  string               `json:"displayColor,omitempty"`
	SortOrder     int                  `json:"sortOrder"`
	OriginalLabel string               `json:"originalLabel,omitempty"`
}

type storedSchedule struct {
	Status         schedule.Status     `json:"status"`
	SourceType     schedule.SourceType `json:"sourceType,omitempty"`
	SourceImportID *uint64             `json:"sourceImportId,omitempty"`
	Note           string              `json:"note"`
	CreatedBy      uint64              `json:"createdBy,omitempty"`
	Segments       []storedSegment     `json:"segments"`
}

type importItemRecord struct {
	ID                 uint64
	Date               schedule.Date
	Type               string
	Draft              *storedSchedule
	ExistingScheduleID *uint64
	ExistingVersion    *uint64
	Decision           string
	Issues             any
	ErrorMessage       string
}

func (s *server) createImport(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, err := s.membership(workspaceID, currentUserID(c))
	if err != nil {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权访问工作区", nil)
		return
	}
	targetUserID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("targetUserId")), 10, 64)
	if err != nil || targetUserID == 0 || !s.userBelongsToWorkspace(workspaceID, targetUserID) {
		failure(c, http.StatusBadRequest, "INVALID_MEMBER", "导入目标成员无效", nil)
		return
	}
	if targetUserID != currentUserID(c) && !requireAdmin(member) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "不能为其他成员导入排班", nil)
		return
	}
	periodStart, startErr := schedule.ParseDate(c.PostForm("periodStart"))
	periodEnd, endErr := schedule.ParseDate(c.PostForm("periodEnd"))
	if startErr != nil || endErr != nil || periodStart.String() > periodEnd.String() {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", "导入日期范围无效", nil)
		return
	}
	periodDates, err := importDates(periodStart, periodEnd)
	if err != nil || len(periodDates) > 366 {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", "导入日期范围不能超过 366 天", nil)
		return
	}
	header, err := c.FormFile("file")
	if err != nil || header.Size <= 0 || header.Size > maxImportBytes {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "请选择不超过 10MB 的排班文件", nil)
		return
	}
	file, err := header.Open()
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "无法读取上传文件", nil)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxImportBytes+1))
	if err != nil || int64(len(content)) > maxImportBytes {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "上传文件读取失败或超过限制", nil)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	var importType string
	var workbook importer.WorkbookData
	switch ext {
	case ".xlsx":
		if len(content) < 4 || !bytes.Equal(content[:2], []byte{'P', 'K'}) {
			failure(c, http.StatusBadRequest, "INVALID_FILE_SIGNATURE", "文件内容不是有效的 XLSX", nil)
			return
		}
		importType = "XLSX"
		workbook, err = importer.ReadXLSX(bytes.NewReader(content), importer.DefaultLimits())
	case ".xls":
		if len(content) < 8 || !bytes.Equal(content[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
			failure(c, http.StatusBadRequest, "INVALID_FILE_SIGNATURE", "文件内容不是有效的 XLS", nil)
			return
		}
		importType = "XLS"
		workbook, err = importer.ReadXLS(bytes.NewReader(content), importer.DefaultLimits())
	default:
		failure(c, http.StatusBadRequest, "UNSUPPORTED_FILE", "当前接口只接受 .xlsx 或 .xls", nil)
		return
	}
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_WORKBOOK", err.Error(), nil)
		return
	}
	mappings, err := s.shiftMappings(c.Request.Context(), workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取班次映射", nil)
		return
	}
	entries, err := importer.Normalize(workbook, mappings)
	if err != nil {
		writeImportValidationFailure(c, err)
		return
	}
	for _, entry := range entries {
		if entry.Date.String() < periodStart.String() || entry.Date.String() > periodEnd.String() {
			failure(c, http.StatusBadRequest, "DATE_OUT_OF_RANGE", "文件中存在超出声明周期的日期", gin.H{"date": entry.Date.String()})
			return
		}
	}

	existing, err := s.querySchedules(workspaceID, []uint64{targetUserID}, periodStart, periodEnd)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取现有排班", nil)
		return
	}
	existingByDate := make(map[schedule.Date]schedule.Day, len(existing))
	for _, day := range existing {
		existingByDate[day.WorkDate] = day
	}

	storageKey := filepath.ToSlash(filepath.Join("imports", uuid.NewString()+ext))
	if err := s.store.Put(c.Request.Context(), storageKey, bytes.NewReader(content)); err != nil {
		failure(c, http.StatusInternalServerError, "STORAGE_ERROR", "无法保存上传文件", nil)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.store.Delete(c.Request.Context(), storageKey)
		}
	}()

	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建导入任务", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_jobs
    (workspace_id, upload_user_id, target_user_id, import_type, state, period_start, period_end, source_filename, idempotency_key)
VALUES (?, ?, ?, ?, 'NEEDS_REVIEW', ?, ?, ?, ?)`, workspaceID, currentUserID(c), targetUserID, importType,
		periodStart.String(), periodEnd.String(), filepath.Base(header.Filename), idempotencyKey)
	if err != nil {
		failure(c, http.StatusConflict, "IMPORT_ALREADY_EXISTS", "相同导入请求已存在", nil)
		return
	}
	jobIDValue, _ := result.LastInsertId()
	jobID := uint64(jobIDValue)
	digest := sha256.Sum256(content)
	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_files (import_job_id, storage_key, original_name, media_type, byte_size, sha256)
VALUES (?, ?, ?, ?, ?, ?)`, jobID, storageKey, filepath.Base(header.Filename), header.Header.Get("Content-Type"), len(content), digest[:]); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录导入文件", nil)
		return
	}
	entriesByDate := make(map[schedule.Date]importer.Entry, len(entries))
	for _, entry := range entries {
		entriesByDate[entry.Date] = entry
	}
	conflictCount, invalidCount := 0, 0
	for index, date := range periodDates {
		entry, found := entriesByDate[date]
		if !found {
			if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_items (import_job_id, work_date, item_type, sort_order)
VALUES (?, ?, 'MISSING', ?)`, jobID, date.String(), index); err != nil {
				failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法生成缺失日期预览", nil)
				return
			}
			continue
		}
		draft := storedFromEntry(entry)
		itemType := "NEW"
		var existingID, existingVersion any
		old, hasExisting := existingByDate[entry.Date]
		if hasExisting {
			existingID, existingVersion = old.ID, old.Version
		}
		if entry.Uncertain {
			itemType = "UNCERTAIN"
			invalidCount++
		} else if hasExisting {
			if schedulesEquivalent(draft, storedFromDay(old)) {
				itemType = "SAME"
			} else {
				itemType = "CONFLICT"
				conflictCount++
			}
		}
		payload, _ := json.Marshal(draft)
		issues, _ := json.Marshal(entry.Issues)
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_items (import_job_id, work_date, item_type, draft_snapshot, existing_schedule_id, existing_version, issues, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, jobID, entry.Date.String(), itemType, payload, existingID, existingVersion, issues, index); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法生成导入预览", nil)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET item_count = ?, conflict_count = ?, invalid_count = ? WHERE id = ?`, len(periodDates), conflictCount, invalidCount, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新导入任务", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入预览", nil)
		return
	}
	committed = true
	s.recordAudit(c, workspaceID, currentUserID(c), "IMPORT_PREVIEW", "import_job", jobID, gin.H{"targetUserId": targetUserID, "itemCount": len(periodDates), "conflictCount": conflictCount})
	success(c, http.StatusCreated, gin.H{"id": jobID, "state": "NEEDS_REVIEW", "itemCount": len(periodDates), "conflictCount": conflictCount, "invalidCount": invalidCount})
}

func importDates(start, end schedule.Date) ([]schedule.Date, error) {
	current, err := time.Parse("2006-01-02", start.String())
	if err != nil {
		return nil, err
	}
	last, err := time.Parse("2006-01-02", end.String())
	if err != nil || last.Before(current) {
		return nil, errors.New("invalid date range")
	}
	result := make([]schedule.Date, 0, int(last.Sub(current).Hours()/24)+1)
	for !current.After(last) {
		result = append(result, schedule.MustDate(current.Format("2006-01-02")))
		current = current.AddDate(0, 0, 1)
	}
	return result, nil
}

func (s *server) createImageImport(c *gin.Context) {
	if !s.config.AIEnabled {
		failure(c, http.StatusServiceUnavailable, "AI_DISABLED", "图片 AI 导入功能未启用", nil)
		return
	}
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, err := s.membership(workspaceID, currentUserID(c))
	if err != nil {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权访问工作区", nil)
		return
	}
	targetUserID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("targetUserId")), 10, 64)
	if err != nil || targetUserID == 0 || !s.userBelongsToWorkspace(workspaceID, targetUserID) {
		failure(c, http.StatusBadRequest, "INVALID_MEMBER", "导入目标成员无效", nil)
		return
	}
	if targetUserID != currentUserID(c) && !requireAdmin(member) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "不能为其他成员导入排班", nil)
		return
	}
	periodStart, startErr := schedule.ParseDate(c.PostForm("periodStart"))
	periodEnd, endErr := schedule.ParseDate(c.PostForm("periodEnd"))
	startTime, parseStartErr := time.Parse("2006-01-02", periodStart.String())
	endTime, parseEndErr := time.Parse("2006-01-02", periodEnd.String())
	if startErr != nil || endErr != nil || parseStartErr != nil || parseEndErr != nil || startTime.After(endTime) || endTime.Sub(startTime) > 365*24*time.Hour {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", "图片识别日期范围无效或超过 366 天", nil)
		return
	}
	instructions := strings.TrimSpace(c.PostForm("instructions"))
	if len([]rune(instructions)) > 2000 {
		failure(c, http.StatusBadRequest, "INVALID_INSTRUCTIONS", "识别说明不能超过 2000 字", nil)
		return
	}
	mappingHints := map[string]string{}
	if raw := strings.TrimSpace(c.PostForm("mappingHints")); raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		if err := decoder.Decode(&mappingHints); err != nil || len(mappingHints) > 100 {
			failure(c, http.StatusBadRequest, "INVALID_MAPPING_HINTS", "班次映射提示格式无效", nil)
			return
		}
	}
	header, err := c.FormFile("file")
	if err != nil || header.Size <= 0 || header.Size > maxImportBytes {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "请选择不超过 10MB 的排班截图", nil)
		return
	}
	file, err := header.Open()
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "无法读取排班截图", nil)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxImportBytes+1))
	if err != nil || int64(len(content)) > maxImportBytes {
		failure(c, http.StatusBadRequest, "INVALID_FILE", "截图读取失败或超过限制", nil)
		return
	}
	imageConfig, imageFormat, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 || int64(imageConfig.Width)*int64(imageConfig.Height) > 25_000_000 {
		failure(c, http.StatusBadRequest, "INVALID_IMAGE", "截图格式无效或超过 2500 万像素", nil)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	expectedFormat := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".webp": "webp", ".gif": "gif"}[ext]
	if expectedFormat == "" || imageFormat != expectedFormat {
		failure(c, http.StatusBadRequest, "INVALID_FILE_SIGNATURE", "截图扩展名与实际格式不一致", nil)
		return
	}
	mediaType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp", "gif": "image/gif"}[imageFormat]
	storageKey := filepath.ToSlash(filepath.Join("imports", uuid.NewString()+ext))
	if err := s.store.Put(c.Request.Context(), storageKey, bytes.NewReader(content)); err != nil {
		failure(c, http.StatusInternalServerError, "STORAGE_ERROR", "无法保存排班截图", nil)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.store.Delete(c.Request.Context(), storageKey)
		}
	}()
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建图片识别任务", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}
	mappingJSON, _ := json.Marshal(mappingHints)
	result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_jobs
    (workspace_id, upload_user_id, target_user_id, import_type, state, period_start, period_end, source_filename, idempotency_key, recognition_instructions, mapping_hints)
VALUES (?, ?, ?, 'IMAGE_AI', 'PENDING', ?, ?, ?, ?, NULLIF(?, ''), ?)`, workspaceID, currentUserID(c), targetUserID,
		periodStart.String(), periodEnd.String(), filepath.Base(header.Filename), idempotencyKey, instructions, mappingJSON)
	if err != nil {
		failure(c, http.StatusConflict, "IMPORT_ALREADY_EXISTS", "相同导入请求已存在", nil)
		return
	}
	jobIDValue, _ := result.LastInsertId()
	jobID := uint64(jobIDValue)
	digest := sha256.Sum256(content)
	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_files (import_job_id, storage_key, original_name, media_type, byte_size, sha256)
VALUES (?, ?, ?, ?, ?, ?)`, jobID, storageKey, filepath.Base(header.Filename), mediaType, len(content), digest[:]); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录导入截图", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交图片识别任务", nil)
		return
	}
	committed = true
	s.recordAudit(c, workspaceID, currentUserID(c), "IMAGE_IMPORT_CREATED", "import_job", jobID, gin.H{"targetUserId": targetUserID})
	if s.importWakeup != nil {
		s.importWakeup()
	}
	success(c, http.StatusAccepted, gin.H{"id": jobID, "state": "PENDING", "itemCount": 0, "conflictCount": 0, "invalidCount": 0})
}

func (s *server) downloadImportFile(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var storageKey, originalName, mediaType string
	if err := s.db.QueryRowContext(c.Request.Context(), `
SELECT f.storage_key, f.original_name, f.media_type FROM import_files f JOIN import_jobs j ON j.id = f.import_job_id
WHERE j.id = ? AND j.workspace_id = ?`, jobID, workspaceID).Scan(&storageKey, &originalName, &mediaType); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_FILE_NOT_FOUND", "导入原文件不存在", nil)
		return
	}
	reader, err := s.store.Open(c.Request.Context(), storageKey)
	if err != nil {
		failure(c, http.StatusNotFound, "IMPORT_FILE_NOT_FOUND", "导入原文件不存在", nil)
		return
	}
	defer reader.Close()
	c.Header("Content-Type", mediaType)
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.QueryEscape(originalName))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func (s *server) cancelImport(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	result, err := s.db.ExecContext(c.Request.Context(), `
UPDATE import_jobs SET state = 'CANCELLED', lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
WHERE id = ? AND workspace_id = ? AND state IN ('UPLOADED', 'PENDING', 'PARSING', 'NEEDS_REVIEW')`, jobID, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法取消导入任务", nil)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		failure(c, http.StatusConflict, "IMPORT_NOT_CANCELLABLE", "导入任务当前不可取消", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "IMPORT_CANCELLED", "import_job", jobID, nil)
	success(c, http.StatusOK, gin.H{"id": jobID, "state": "CANCELLED"})
}

func (s *server) downloadImportTemplate(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	shiftNames, err := s.enabledShiftNames(c.Request.Context(), workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "TEMPLATE_ERROR", "无法读取启用班次", nil)
		return
	}
	workbook, err := newScheduleImportTemplate(shiftNames)
	if err != nil {
		failure(c, http.StatusInternalServerError, "TEMPLATE_ERROR", "无法生成 Excel 模板", nil)
		return
	}
	defer workbook.Close()
	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		failure(c, http.StatusInternalServerError, "TEMPLATE_ERROR", "无法生成 Excel 模板", nil)
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=shiftory-schedule-template.xlsx")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buffer.Bytes())
}

func newScheduleImportTemplate(shiftNames []string) (*excelize.File, error) {
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	if err := workbook.SetSheetName(sheet, "排班导入"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	sheet = "排班导入"
	rows := [][]any{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-01", "工作", "早班", "", "", "否", ""},
		{"2026-09-02", "休息", "", "", "", "否", ""},
		{"2026-09-03", "工作", "", "08:30", "17:30", "否", ""},
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				_ = workbook.Close()
				return nil, err
			}
			if err := workbook.SetCellValue(sheet, cell, value); err != nil {
				_ = workbook.Close()
				return nil, err
			}
		}
	}
	textStyle, err := workbook.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetCellStyle(sheet, "A2", "A1000", textStyle); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetColWidth(sheet, "A", "A", 14); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetColWidth(sheet, "B", "G", 13); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	optionsSheet := "模板选项"
	if _, err := workbook.NewSheet(optionsSheet); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetCellValue(optionsSheet, "A1", "工作"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetCellValue(optionsSheet, "A2", "休息"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetCellValue(optionsSheet, "C1", "否"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := workbook.SetCellValue(optionsSheet, "C2", "是"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	cleanedShifts := make([]string, 0, len(shiftNames))
	seenShifts := make(map[string]struct{}, len(shiftNames))
	for _, name := range shiftNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := seenShifts[name]; exists {
			continue
		}
		seenShifts[name] = struct{}{}
		cleanedShifts = append(cleanedShifts, name)
	}
	if len(cleanedShifts) == 0 {
		cleanedShifts = []string{"（暂无启用班次）"}
	}
	for index, name := range cleanedShifts {
		cell, err := excelize.CoordinatesToCellName(2, index+1)
		if err != nil {
			_ = workbook.Close()
			return nil, err
		}
		if err := workbook.SetCellValue(optionsSheet, cell, name); err != nil {
			_ = workbook.Close()
			return nil, err
		}
	}
	if err := workbook.SetSheetVisible(optionsSheet, false, true); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := addTemplateListValidation(workbook, sheet, "B2:B1000", "'模板选项'!$A$1:$A$2", "状态", "请选择工作或休息"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := addTemplateListValidation(workbook, sheet, "C2:C1000", "'模板选项'!$B$1:$B$"+strconv.Itoa(len(cleanedShifts)), "班次", "请选择已配置的启用班次"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	if err := addTemplateListValidation(workbook, sheet, "F2:F1000", "'模板选项'!$C$1:$C$2", "是否跨日", "请选择是或否"); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	// DataValidation stores Formula1 as inner XML, so comparison operators must
	// be escaped before the workbook is serialized.
	formula := `=OR(AND($B2="",$C2="",$D2="",$E2="",$F2=""),AND($B2="休息",$C2="",$D2="",$E2="",OR($F2="",$F2="否")),AND($B2="工作",$C2&lt;&gt;"",$D2="",$E2=""),AND($B2="工作",$C2="",$D2="",$E2="",OR($F2="",$F2="否")),AND($B2="工作",$C2="",$D2&lt;&gt;"",$E2&lt;&gt;""))`
	if err := addTemplateCustomValidation(workbook, sheet, "B2:G1000", formula); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	return workbook, nil
}

func addTemplateListValidation(workbook *excelize.File, sheet, sqref, source, title, prompt string) error {
	dv := excelize.NewDataValidation(true)
	dv.Sqref = sqref
	dv.SetSqrefDropList(source)
	dv.SetError(excelize.DataValidationErrorStyleStop, "输入无效", "shiftory 模板：请选择下拉选项")
	dv.SetInput(title, prompt)
	return workbook.AddDataValidation(sheet, dv)
}

func addTemplateCustomValidation(workbook *excelize.File, sheet, sqref, formula string) error {
	dv := excelize.NewDataValidation(true)
	dv.Sqref = sqref
	if err := dv.SetRange(formula, "", excelize.DataValidationTypeCustom, excelize.DataValidationOperatorBetween); err != nil {
		return err
	}
	dv.SetError(excelize.DataValidationErrorStyleStop, "组合无效", "shiftory 模板：休息、班次、起止时间和跨日设置不符合规则")
	dv.SetInput("排班组合校验", "班次与自定义时间互斥，开始和结束时间必须同时填写")
	return workbook.AddDataValidation(sheet, dv)
}

func (s *server) enabledShiftNames(ctx context.Context, workspaceID uint64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name FROM shifts WHERE workspace_id = ? AND enabled = TRUE ORDER BY sort_order, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *server) shiftMappings(ctx context.Context, workspaceID uint64) (map[string]importer.ShiftMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, code, TIME_FORMAT(start_time, '%H:%i'), TIME_FORMAT(end_time, '%H:%i'), cross_day, display_color
FROM shifts WHERE workspace_id = ? AND enabled = TRUE`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mappings := map[string]importer.ShiftMapping{}
	byID := map[uint64]importer.ShiftMapping{}
	for rows.Next() {
		var mapping importer.ShiftMapping
		var start, end sql.NullString
		if err := rows.Scan(&mapping.ID, &mapping.Name, &mapping.Code, &start, &end, &mapping.CrossDay, &mapping.DisplayColor); err != nil {
			return nil, err
		}
		if start.Valid {
			startValue, endValue := start.String, end.String
			mapping.StartTime, mapping.EndTime = &startValue, &endValue
		}
		mappings[mapping.Name], mappings[mapping.Code] = mapping, mapping
		byID[mapping.ID] = mapping
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	aliasRows, err := s.db.QueryContext(ctx, `SELECT shift_id, alias FROM shift_aliases WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var shiftID uint64
		var alias string
		if err := aliasRows.Scan(&shiftID, &alias); err != nil {
			return nil, err
		}
		if mapping, found := byID[shiftID]; found {
			mappings[alias] = mapping
		}
	}
	return mappings, aliasRows.Err()
}

func (s *server) listImports(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return
	}
	query := `
SELECT id, target_user_id, import_type, state, DATE_FORMAT(period_start, '%Y-%m-%d'), DATE_FORMAT(period_end, '%Y-%m-%d'),
       source_filename, item_count, conflict_count, invalid_count, created_at, completed_at, rolled_back_at
FROM import_jobs WHERE workspace_id = ?`
	args := []any{workspaceID}
	if !requireAdmin(member) {
		query += " AND upload_user_id = ?"
		args = append(args, currentUserID(c))
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询导入记录", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		item, err := scanImportSummary(rows)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入记录", nil)
			return
		}
		items = append(items, item)
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

type rowScanner interface{ Scan(...any) error }

func scanImportSummary(row rowScanner) (gin.H, error) {
	var id, target uint64
	var importType, state, start, end, filename string
	var itemCount, conflictCount, invalidCount int
	var created time.Time
	var completed, rolledBack sql.NullTime
	if err := row.Scan(&id, &target, &importType, &state, &start, &end, &filename, &itemCount, &conflictCount, &invalidCount, &created, &completed, &rolledBack); err != nil {
		return nil, err
	}
	result := gin.H{"id": id, "targetUserId": target, "importType": importType, "state": state, "periodStart": start, "periodEnd": end,
		"sourceFilename": filename, "itemCount": itemCount, "conflictCount": conflictCount, "invalidCount": invalidCount, "createdAt": created}
	if completed.Valid {
		result["completedAt"] = completed.Time
	}
	if rolledBack.Valid {
		result["rolledBackAt"] = rolledBack.Time
	}
	return result, nil
}

func (s *server) getImport(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	jobID, ok := parseID(c, "importId")
	if !ok {
		return
	}
	if !s.requireImportAccess(c, workspaceID, jobID) {
		return
	}
	row := s.db.QueryRowContext(c.Request.Context(), `
SELECT id, target_user_id, import_type, state, DATE_FORMAT(period_start, '%Y-%m-%d'), DATE_FORMAT(period_end, '%Y-%m-%d'),
       source_filename, item_count, conflict_count, invalid_count, created_at, completed_at, rolled_back_at
FROM import_jobs WHERE id = ? AND workspace_id = ?`, jobID, workspaceID)
	job, err := scanImportSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入任务", nil)
		return
	}
	items, err := s.loadImportItems(c.Request.Context(), s.db, jobID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入预览", nil)
		return
	}
	job["items"] = importItemResponses(items)
	success(c, http.StatusOK, job)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *server) loadImportItems(ctx context.Context, db queryer, jobID uint64) ([]importItemRecord, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, DATE_FORMAT(work_date, '%Y-%m-%d'), item_type, draft_snapshot, existing_schedule_id, existing_version, COALESCE(decision, ''), issues, error_message
FROM import_items WHERE import_job_id = ? ORDER BY sort_order, work_date`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]importItemRecord, 0)
	for rows.Next() {
		var item importItemRecord
		var date string
		var draft []byte
		var existingID, existingVersion sql.NullInt64
		var issues []byte
		var errorMessage sql.NullString
		if err := rows.Scan(&item.ID, &date, &item.Type, &draft, &existingID, &existingVersion, &item.Decision, &issues, &errorMessage); err != nil {
			return nil, err
		}
		item.Date = schedule.MustDate(date)
		if len(draft) > 0 {
			var snapshot storedSchedule
			if err := json.Unmarshal(draft, &snapshot); err != nil {
				return nil, err
			}
			item.Draft = &snapshot
		}
		if existingID.Valid {
			value := uint64(existingID.Int64)
			item.ExistingScheduleID = &value
		}
		if existingVersion.Valid {
			value := uint64(existingVersion.Int64)
			item.ExistingVersion = &value
		}
		if len(issues) > 0 {
			if err := json.Unmarshal(issues, &item.Issues); err != nil {
				return nil, err
			}
		}
		if errorMessage.Valid {
			item.ErrorMessage = errorMessage.String
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func importItemResponses(items []importItemRecord) []gin.H {
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		response := gin.H{"id": item.ID, "workDate": item.Date.String(), "type": item.Type, "decision": item.Decision, "draft": item.Draft}
		if item.Issues != nil {
			response["issues"] = item.Issues
		}
		if item.ErrorMessage != "" {
			response["errorMessage"] = item.ErrorMessage
		}
		if item.ExistingScheduleID != nil {
			response["existingScheduleId"] = *item.ExistingScheduleID
			response["existingVersion"] = *item.ExistingVersion
		}
		result = append(result, response)
	}
	return result
}

func (s *server) updateImportDecisions(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var request struct {
		Decisions []struct {
			ItemID   uint64 `json:"itemId" binding:"required"`
			Decision string `json:"decision" binding:"required"`
		} `json:"decisions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_DECISIONS", "冲突决策格式无效", nil)
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存冲突决策", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT state FROM import_jobs WHERE id = ? AND workspace_id = ? FOR UPDATE`, jobID, workspaceID).Scan(&state); err != nil || state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_REVIEWABLE", "导入任务当前不可修改", nil)
		return
	}
	for _, decision := range request.Decisions {
		if decision.Decision != "KEEP_EXISTING" && decision.Decision != "USE_IMPORTED" && decision.Decision != "SKIP" {
			failure(c, http.StatusBadRequest, "INVALID_DECISION", "冲突决策值无效", nil)
			return
		}
		result, err := tx.ExecContext(c.Request.Context(), `
UPDATE import_items SET decision = ? WHERE id = ? AND import_job_id = ? AND item_type IN ('CONFLICT', 'NEW')`, decision.Decision, decision.ItemID, jobID)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存冲突决策", nil)
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			failure(c, http.StatusBadRequest, "INVALID_IMPORT_ITEM", "导入条目不存在或无需决策", nil)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交冲突决策", nil)
		return
	}
	success(c, http.StatusOK, gin.H{"updated": len(request.Decisions)})
}

// correctImportItem replaces an uncertain, invalid, missing, or otherwise
// reviewable draft with a fully validated human-entered schedule. The current
// schedule version becomes the new preview baseline so later changes are still
// rejected by commitImport as stale.
func (s *server) correctImportItem(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	itemID, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil || itemID == 0 {
		failure(c, http.StatusBadRequest, "INVALID_IMPORT_ITEM", "导入条目无效", nil)
		return
	}
	var request scheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_SCHEDULE", "排班信息不完整", nil)
		return
	}

	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法开始预览修正", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetUserID uint64
	var importType, state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, import_type, state FROM import_jobs WHERE id = ? AND workspace_id = ? FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &importType, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_REVIEWABLE", "导入任务当前不可修改", nil)
		return
	}
	var dateText string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT DATE_FORMAT(work_date, '%Y-%m-%d') FROM import_items WHERE id = ? AND import_job_id = ? FOR UPDATE`, itemID, jobID).Scan(&dateText); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_IMPORT_ITEM", "导入条目不存在", nil)
		return
	}
	date := schedule.MustDate(dateText)
	segments := make([]schedule.Segment, 0, len(request.Segments))
	for index, item := range request.Segments {
		segment := schedule.Segment{Type: item.Type, ShiftID: item.ShiftID, CrossDay: item.CrossDay, SortOrder: index}
		switch item.Type {
		case schedule.SegmentShift:
			if item.ShiftID == nil || s.loadShiftSnapshot(workspaceID, *item.ShiftID, &segment) != nil {
				failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次不存在或已禁用", nil)
				return
			}
		case schedule.SegmentTimeRange:
			if item.StartTime != nil {
				clock, parseErr := schedule.ParseClock(*item.StartTime)
				if parseErr != nil {
					failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "开始时间无效", nil)
					return
				}
				segment.StartTime = &clock
			}
			if item.EndTime != nil {
				clock, parseErr := schedule.ParseClock(*item.EndTime)
				if parseErr != nil {
					failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "结束时间无效", nil)
					return
				}
				segment.EndTime = &clock
			}
		default:
			failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "时间段类型无效", nil)
			return
		}
		segments = append(segments, segment)
	}
	day := schedule.Day{WorkspaceID: workspaceID, UserID: targetUserID, WorkDate: date, Status: request.Status,
		SourceType: schedule.SourceType(importType), SourceImportID: &jobID, Note: strings.TrimSpace(request.Note), CreatedBy: currentUserID(c), Segments: segments}
	if err := day.Validate(); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_SCHEDULE", err.Error(), nil)
		return
	}
	draft := storedFromDay(day)
	draftJSON, _ := json.Marshal(draft)
	existing, found, err := queryScheduleTx(c.Request.Context(), tx, workspaceID, targetUserID, date, true)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取现有排班", nil)
		return
	}
	itemType := "NEW"
	var existingID, existingVersion any
	if found {
		existingID, existingVersion = existing.ID, existing.Version
		if schedulesEquivalent(draft, storedFromDay(existing)) {
			itemType = "SAME"
		} else {
			itemType = "CONFLICT"
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE import_items
SET item_type = ?, draft_snapshot = ?, existing_schedule_id = ?, existing_version = ?, decision = NULL, issues = NULL, error_message = NULL
WHERE id = ? AND import_job_id = ?`, itemType, draftJSON, existingID, existingVersion, itemID, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存预览修正", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE import_jobs
SET conflict_count = (SELECT COUNT(*) FROM import_items WHERE import_job_id = ? AND item_type = 'CONFLICT'),
    invalid_count = (SELECT COUNT(*) FROM import_items WHERE import_job_id = ? AND item_type IN ('INVALID', 'UNCERTAIN'))
WHERE id = ?`, jobID, jobID, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新导入统计", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交预览修正", nil)
		return
	}
	response := gin.H{"id": itemID, "workDate": date.String(), "type": itemType, "decision": "", "draft": draft}
	if found {
		response["existingScheduleId"], response["existingVersion"] = existing.ID, existing.Version
	}
	success(c, http.StatusOK, response)
}

func (s *server) parseImportScope(c *gin.Context) (uint64, uint64, bool) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return 0, 0, false
	}
	jobID, ok := parseID(c, "importId")
	if !ok {
		return 0, 0, false
	}
	if !s.requireImportAccess(c, workspaceID, jobID) {
		return 0, 0, false
	}
	return workspaceID, jobID, true
}

func (s *server) requireImportAccess(c *gin.Context, workspaceID, jobID uint64) bool {
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return false
	}
	var uploaderID uint64
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT upload_user_id FROM import_jobs WHERE id = ? AND workspace_id = ?`, jobID, workspaceID).Scan(&uploaderID); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return false
	}
	if !requireAdmin(member) && uploaderID != currentUserID(c) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权查看或操作该导入任务", nil)
		return false
	}
	return true
}

func (s *server) commitImport(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetUserID uint64
	var importType, state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, import_type, state FROM import_jobs WHERE id = ? AND workspace_id = ? FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &importType, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if state == "COMPLETED" {
		success(c, http.StatusOK, gin.H{"id": jobID, "state": state})
		return
	}
	if state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_COMMITTABLE", "导入任务当前不可提交", nil)
		return
	}
	items, err := s.loadImportItems(c.Request.Context(), tx, jobID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入预览", nil)
		return
	}
	for _, item := range items {
		if item.Type == "CONFLICT" && item.Decision == "" {
			failure(c, http.StatusConflict, "UNRESOLVED_CONFLICTS", "仍有冲突尚未处理", gin.H{"itemId": item.ID})
			return
		}
		if item.Type != "NEW" && !(item.Type == "CONFLICT" && item.Decision == "USE_IMPORTED") {
			continue
		}
		if item.Decision == "SKIP" || item.Draft == nil {
			continue
		}
		current, found, err := queryScheduleTx(c.Request.Context(), tx, workspaceID, targetUserID, item.Date, true)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法锁定现有排班", nil)
			return
		}
		if item.ExistingScheduleID == nil {
			if found {
				failure(c, http.StatusConflict, "STALE_PREVIEW", "预览后排班已发生变化，请重新导入", gin.H{"workDate": item.Date.String()})
				return
			}
		} else if !found || current.ID != *item.ExistingScheduleID || current.Version != *item.ExistingVersion {
			failure(c, http.StatusConflict, "STALE_PREVIEW", "预览后排班已发生变化，请重新导入", gin.H{"workDate": item.Date.String()})
			return
		}
		var beforeJSON any
		var beforeVersion any
		if found {
			payload, _ := json.Marshal(storedFromDay(current))
			beforeJSON, beforeVersion = payload, current.Version
		}
		importID := jobID
		day := dayFromStored(*item.Draft, workspaceID, targetUserID, item.Date)
		day.SourceType, day.SourceImportID, day.CreatedBy = schedule.SourceType(importType), &importID, currentUserID(c)
		saved, err := writeScheduleTx(c.Request.Context(), tx, day, current, found)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法写入导入排班", gin.H{"reason": err.Error()})
			return
		}
		afterJSON, _ := json.Marshal(storedFromDay(saved))
		changeType := "IMPORT_CREATE"
		if found {
			changeType = "IMPORT_UPDATE"
		}
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, import_job_id, changed_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, saved.ID, workspaceID, targetUserID, item.Date.String(), beforeVersion, saved.Version, beforeJSON, afterJSON, changeType, jobID, currentUserID(c)); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录导入版本", nil)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state = 'COMPLETED', completed_at = UTC_TIMESTAMP(6) WHERE id = ?`, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法完成导入任务", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入事务", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "IMPORT_COMMIT", "import_job", jobID, gin.H{"targetUserId": targetUserID})
	success(c, http.StatusOK, gin.H{"id": jobID, "state": "COMPLETED"})
}

func (s *server) rollbackImport(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法回滚导入", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetUserID uint64
	var state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, state FROM import_jobs WHERE id = ? AND workspace_id = ? FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if state == "ROLLED_BACK" {
		success(c, http.StatusOK, gin.H{"id": jobID, "state": state})
		return
	}
	if state != "COMPLETED" {
		failure(c, http.StatusConflict, "IMPORT_NOT_ROLLBACKABLE", "只有已完成导入可以回滚", nil)
		return
	}
	rows, err := tx.QueryContext(c.Request.Context(), `
SELECT DATE_FORMAT(work_date, '%Y-%m-%d'), before_snapshot, after_snapshot, after_version
FROM schedule_revisions WHERE import_job_id = ? AND change_type IN ('IMPORT_CREATE', 'IMPORT_UPDATE') ORDER BY id DESC`, jobID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入版本", nil)
		return
	}
	type revision struct {
		date          schedule.Date
		before, after []byte
		afterVersion  uint64
	}
	revisions := make([]revision, 0)
	for rows.Next() {
		var item revision
		var date string
		if err := rows.Scan(&date, &item.before, &item.after, &item.afterVersion); err != nil {
			rows.Close()
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入版本", nil)
			return
		}
		item.date = schedule.MustDate(date)
		revisions = append(revisions, item)
	}
	rows.Close()
	for _, revision := range revisions {
		current, found, err := queryScheduleTx(c.Request.Context(), tx, workspaceID, targetUserID, revision.date, true)
		if err != nil || !found || current.Version != revision.afterVersion || current.SourceImportID == nil || *current.SourceImportID != jobID {
			failure(c, http.StatusConflict, "ROLLBACK_CONFLICT", "导入后的排班已被修改，不能自动回滚", gin.H{"workDate": revision.date.String()})
			return
		}
		if len(revision.before) == 0 {
			if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM schedule_days WHERE id = ?`, current.ID); err != nil {
				failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法删除导入新增排班", nil)
				return
			}
			if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, import_job_id, changed_by)
VALUES (NULL, ?, ?, ?, ?, NULL, ?, NULL, 'IMPORT_ROLLBACK_DELETE', ?, ?)`, workspaceID, targetUserID, revision.date.String(), current.Version, revision.after, jobID, currentUserID(c)); err != nil {
				failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录回滚版本", nil)
				return
			}
			continue
		}
		var prior storedSchedule
		if err := json.Unmarshal(revision.before, &prior); err != nil {
			failure(c, http.StatusInternalServerError, "INVALID_REVISION", "历史排班快照损坏", nil)
			return
		}
		restored := dayFromStored(prior, workspaceID, targetUserID, revision.date)
		restored.ID, restored.Version = current.ID, current.Version+1
		if _, err := writeScheduleTx(c.Request.Context(), tx, restored, current, true); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法恢复历史排班", nil)
			return
		}
		restored.Version = current.Version + 1
		restoredJSON, _ := json.Marshal(storedFromDay(restored))
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, import_job_id, changed_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'IMPORT_ROLLBACK_RESTORE', ?, ?)`, current.ID, workspaceID, targetUserID, revision.date.String(), current.Version, restored.Version, revision.after, restoredJSON, jobID, currentUserID(c)); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录回滚版本", nil)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state = 'ROLLED_BACK', rolled_back_at = UTC_TIMESTAMP(6) WHERE id = ?`, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法完成回滚", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交回滚事务", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "IMPORT_ROLLBACK", "import_job", jobID, nil)
	success(c, http.StatusOK, gin.H{"id": jobID, "state": "ROLLED_BACK"})
}

func storedFromEntry(entry importer.Entry) storedSchedule {
	day := schedule.Day{Status: entry.Status, Note: entry.Note, Segments: entry.Segments}
	return storedFromDay(day)
}

func storedFromDay(day schedule.Day) storedSchedule {
	segments := make([]storedSegment, 0, len(day.Segments))
	ordered := append([]schedule.Segment(nil), day.Segments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SortOrder < ordered[j].SortOrder })
	for _, segment := range ordered {
		stored := storedSegment{Type: segment.Type, ShiftID: segment.ShiftID, ShiftName: segment.ShiftName, ShiftCode: segment.ShiftCode,
			CrossDay: segment.CrossDay, DisplayColor: segment.DisplayColor, SortOrder: segment.SortOrder, OriginalLabel: segment.OriginalLabel}
		if segment.StartTime != nil {
			stored.StartTime, stored.EndTime = segment.StartTime.String(), segment.EndTime.String()
		}
		segments = append(segments, stored)
	}
	return storedSchedule{Status: day.Status, SourceType: day.SourceType, SourceImportID: day.SourceImportID, Note: day.Note, CreatedBy: day.CreatedBy, Segments: segments}
}

func dayFromStored(stored storedSchedule, workspaceID, userID uint64, date schedule.Date) schedule.Day {
	day := schedule.Day{WorkspaceID: workspaceID, UserID: userID, WorkDate: date, Status: stored.Status, SourceType: stored.SourceType,
		SourceImportID: stored.SourceImportID, Note: stored.Note, CreatedBy: stored.CreatedBy, Segments: make([]schedule.Segment, 0, len(stored.Segments))}
	for _, item := range stored.Segments {
		segment := schedule.Segment{Type: item.Type, ShiftID: item.ShiftID, ShiftName: item.ShiftName, ShiftCode: item.ShiftCode, CrossDay: item.CrossDay,
			DisplayColor: item.DisplayColor, SortOrder: item.SortOrder, OriginalLabel: item.OriginalLabel}
		if item.StartTime != "" {
			start, _ := schedule.ParseClock(item.StartTime)
			end, _ := schedule.ParseClock(item.EndTime)
			segment.StartTime, segment.EndTime = &start, &end
		}
		day.Segments = append(day.Segments, segment)
	}
	return day
}

func schedulesEquivalent(left, right storedSchedule) bool {
	if left.Status != right.Status || strings.TrimSpace(left.Note) != strings.TrimSpace(right.Note) || len(left.Segments) != len(right.Segments) {
		return false
	}
	for index := range left.Segments {
		a, b := left.Segments[index], right.Segments[index]
		if a.Type != b.Type || !equalUintPtr(a.ShiftID, b.ShiftID) || a.StartTime != b.StartTime || a.EndTime != b.EndTime || a.CrossDay != b.CrossDay {
			return false
		}
	}
	return true
}

func equalUintPtr(left, right *uint64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func queryScheduleTx(ctx context.Context, tx *sql.Tx, workspaceID, userID uint64, date schedule.Date, lock bool) (schedule.Day, bool, error) {
	query := `
SELECT id, status, source_type, source_import_id, note, version, created_by
FROM schedule_days WHERE workspace_id = ? AND user_id = ? AND work_date = ?`
	if lock {
		query += " FOR UPDATE"
	}
	day := schedule.Day{WorkspaceID: workspaceID, UserID: userID, WorkDate: date}
	var importID sql.NullInt64
	err := tx.QueryRowContext(ctx, query, workspaceID, userID, date.String()).Scan(&day.ID, &day.Status, &day.SourceType, &importID, &day.Note, &day.Version, &day.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return schedule.Day{}, false, nil
	}
	if err != nil {
		return schedule.Day{}, false, err
	}
	if importID.Valid {
		value := uint64(importID.Int64)
		day.SourceImportID = &value
	}
	segments, err := querySegmentsTx(ctx, tx, day.ID)
	if err != nil {
		return schedule.Day{}, false, err
	}
	day.Segments = segments
	return day, true, nil
}

func querySegmentsTx(ctx context.Context, tx *sql.Tx, dayID uint64) ([]schedule.Segment, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, segment_type, shift_id, COALESCE(shift_name_snapshot, ''), COALESCE(shift_code_snapshot, ''),
       TIME_FORMAT(start_time, '%H:%i'), TIME_FORMAT(end_time, '%H:%i'), cross_day,
       COALESCE(display_color_snapshot, ''), sort_order, COALESCE(original_label, '')
FROM schedule_segments WHERE schedule_day_id = ? ORDER BY sort_order, id`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	segments := make([]schedule.Segment, 0)
	for rows.Next() {
		var segment schedule.Segment
		var shiftID sql.NullInt64
		var start, end sql.NullString
		if err := rows.Scan(&segment.ID, &segment.Type, &shiftID, &segment.ShiftName, &segment.ShiftCode, &start, &end, &segment.CrossDay,
			&segment.DisplayColor, &segment.SortOrder, &segment.OriginalLabel); err != nil {
			return nil, err
		}
		if shiftID.Valid {
			value := uint64(shiftID.Int64)
			segment.ShiftID = &value
		}
		if start.Valid {
			startClock, _ := schedule.ParseClock(start.String)
			endClock, _ := schedule.ParseClock(end.String)
			segment.StartTime, segment.EndTime = &startClock, &endClock
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func writeScheduleTx(ctx context.Context, tx *sql.Tx, day, existing schedule.Day, found bool) (schedule.Day, error) {
	if found {
		day.ID, day.Version = existing.ID, existing.Version+1
		_, err := tx.ExecContext(ctx, `
UPDATE schedule_days SET status = ?, source_type = ?, source_import_id = ?, note = ?, version = ? WHERE id = ? AND version = ?`,
			day.Status, day.SourceType, day.SourceImportID, day.Note, day.Version, day.ID, existing.Version)
		if err != nil {
			return schedule.Day{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_segments WHERE schedule_day_id = ?`, day.ID); err != nil {
			return schedule.Day{}, err
		}
	} else {
		day.Version = 1
		result, err := tx.ExecContext(ctx, `
INSERT INTO schedule_days (workspace_id, user_id, work_date, status, source_type, source_import_id, note, version, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`, day.WorkspaceID, day.UserID, day.WorkDate.String(), day.Status, day.SourceType, day.SourceImportID, day.Note, day.CreatedBy)
		if err != nil {
			return schedule.Day{}, err
		}
		id, _ := result.LastInsertId()
		day.ID = uint64(id)
	}
	for _, segment := range day.Segments {
		var start, end any
		if segment.StartTime != nil {
			start, end = segment.StartTime.String()+":00", segment.EndTime.String()+":00"
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO schedule_segments
    (schedule_day_id, segment_type, shift_id, shift_name_snapshot, shift_code_snapshot, start_time, end_time, cross_day, display_color_snapshot, sort_order, original_label)
VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''))`, day.ID, segment.Type, segment.ShiftID,
			segment.ShiftName, segment.ShiftCode, start, end, segment.CrossDay, segment.DisplayColor, segment.SortOrder, segment.OriginalLabel); err != nil {
			return schedule.Day{}, err
		}
	}
	return day, nil
}
