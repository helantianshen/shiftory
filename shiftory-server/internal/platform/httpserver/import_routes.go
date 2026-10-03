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
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/schedule"
)

const maxImportBytes = int64(10 << 20)

// storedSegment 保存可序列化的班次和时间段快照
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

// storedSchedule 保存排班业务快照与来源信息，供预览比较和修订恢复
type storedSchedule struct {
	RuleIDs        []string            `json:"ruleIds,omitempty"`
	Status         schedule.Status     `json:"status"`
	SourceType     schedule.SourceType `json:"sourceType,omitempty"`
	SourceImportID *uint64             `json:"sourceImportId,omitempty"`
	Note           string              `json:"note"`
	CreatedBy      uint64              `json:"createdBy,omitempty"`
	Segments       []storedSegment     `json:"segments"`
}

// importItemRecord 保存导入草稿、人工决定与预览生成时的排班版本
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

// createImport 校验 Excel 上传并同步生成逐日预览，原文件与任务保存成功后返回待审核状态
func (s *server) createImport(c *gin.Context) {
	s.logger.DebugContext(c.Request.Context(), "spreadsheet upload received", "path", c.Request.URL.Path)
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
	// 授权与目标成员校验后限定导入区间，防止文件数据超出用户声明范围
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
	// 先限制上传体积，再按扩展名与文件签名选择对应工作簿读取器
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
	// 加载工作区班次进行领域规范化，将校验问题映射为可修复的行级错误
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
	s.logger.DebugContext(c.Request.Context(), "spreadsheet upload normalized", "workspace_id", workspaceID, "target_user_id", targetUserID, "filename", filepath.Base(header.Filename), "bytes", len(content), "entry_count", len(entries))
	for _, entry := range entries {
		if entry.Date.String() < periodStart.String() || entry.Date.String() > periodEnd.String() {
			failure(c, http.StatusBadRequest, "DATE_OUT_OF_RANGE", "文件中存在超出声明周期的日期", gin.H{"date": entry.Date.String()})
			return
		}
	}

	// 读取现有全局排班作为预览基线，正式提交时仍需重新校验版本
	existing, err := s.querySchedules(workspaceID, []uint64{targetUserID}, periodStart, periodEnd)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取现有排班", nil)
		return
	}
	existingByDate := make(map[schedule.Date]schedule.Day, len(existing))
	for _, day := range existing {
		existingByDate[day.WorkDate] = day
	}

	// 原文件先写存储，数据库未提交时尝试补偿删除，两个资源不共享事务
	storageKey := filepath.ToSlash(filepath.Join("imports", uuid.NewString()+ext))
	if err := s.store.Put(c.Request.Context(), storageKey, bytes.NewReader(content)); err != nil {
		failure(c, http.StatusInternalServerError, "STORAGE_ERROR", "无法保存上传文件", nil)
		return
	}
	committed := false
	defer func() {
		// 文件系统不参与数据库事务，提交前失败时必须补偿删除已写入文件
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
	var jobIDValue int64
	err = tx.QueryRowContext(c.Request.Context(), `
INSERT INTO import_jobs
    (workspace_id, upload_user_id, target_user_id, import_type, state, period_start, period_end, source_filename, idempotency_key)
VALUES ($1, $2, $3, $4, 'NEEDS_REVIEW', $5, $6, $7, $8) RETURNING id`, workspaceID, currentUserID(c), targetUserID, importType,
		periodStart.String(), periodEnd.String(), filepath.Base(header.Filename), idempotencyKey).Scan(&jobIDValue)
	if err != nil {
		failure(c, http.StatusConflict, "IMPORT_ALREADY_EXISTS", "相同导入请求已存在", nil)
		return
	}

	jobID := uint64(jobIDValue)
	digest := sha256.Sum256(content)
	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO import_files (import_job_id, storage_key, original_name, media_type, byte_size, sha256)
VALUES ($1, $2, $3, $4, $5, $6)`, jobID, storageKey, filepath.Base(header.Filename), header.Header.Get("Content-Type"), len(content), digest[:]); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录导入文件", nil)
		return
	}
	// 按完整声明区间生成预览，文件缺少的日期单独保存为缺失项
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
VALUES ($1, $2, 'MISSING', $3)`, jobID, date.String(), index); err != nil {
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, jobID, entry.Date.String(), itemType, payload, existingID, existingVersion, issues, index); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法生成导入预览", nil)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET item_count = $1, conflict_count = $2, invalid_count = $3 WHERE id = $4`, len(periodDates), conflictCount, invalidCount, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新导入任务", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入预览", nil)
		return
	}
	committed = true
	s.logger.InfoContext(c.Request.Context(), "spreadsheet import job created", "workspace_id", workspaceID, "job_id", jobID, "target_user_id", targetUserID, "filename", filepath.Base(header.Filename), "bytes", len(content), "item_count", len(periodDates), "conflict_count", conflictCount, "invalid_count", invalidCount)
	s.recordAudit(c, workspaceID, currentUserID(c), "IMPORT_PREVIEW", "import_job", jobID, gin.H{"targetUserId": targetUserID, "itemCount": len(periodDates), "conflictCount": conflictCount})
	success(c, http.StatusCreated, gin.H{"id": jobID, "state": "NEEDS_REVIEW", "itemCount": len(periodDates), "conflictCount": conflictCount, "invalidCount": invalidCount})
}

// importDates 展开包含首尾的导入日期范围，天数上限由调用方检查
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

// createImageImport 校验图片、目标用户及周期后创建持久化任务，提交后通知内置 Runner
func (s *server) createImageImport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes+(128<<10))
	s.logger.DebugContext(c.Request.Context(), "image upload received", "path", c.Request.URL.Path)
	if !s.config.AIEnabled {
		s.logger.WarnContext(c.Request.Context(), "image upload rejected because AI is disabled")
		failure(c, http.StatusServiceUnavailable, "AI_DISABLED", "图片 AI 导入功能未启用", nil)
		return
	}
	if len(s.config.AI.Providers) > 0 && !s.config.AI.Supports(true) {
		failure(c, 503, "NO_AVAILABLE_PROVIDER", "没有配置支持图片的启用模型", nil)
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
	in := aiImportInput{TargetUserID: targetUserID, PeriodStart: periodStart.String(), PeriodEnd: periodEnd.String(), Instructions: instructions, MappingHints: mappingHints}
	if !s.validateAIInput(c, workspaceID, in) {
		return
	}
	snapshot, err := importjob.FreezeInput(c.Request.Context(), s.db, workspaceID)
	if err != nil {
		failure(c, 500, "SNAPSHOT_ERROR", "无法固定识别输入", nil)
		return
	}

	// 图片上传先进行体积与格式检查，外部模型识别由异步任务执行
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
	s.logger.DebugContext(c.Request.Context(), "image upload validated", "workspace_id", workspaceID, "target_user_id", targetUserID,
		"bytes", len(content), "width", imageConfig.Width, "height", imageConfig.Height,
		"format", imageFormat, "instructions_length", len([]rune(instructions)), "mapping_hint_count", len(mappingHints))
	mediaType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp", "gif": "image/gif"}[imageFormat]
	// 先保存图片再写任务元数据，数据库失败时通过补偿删除减少孤立文件
	storageKey := filepath.ToSlash(filepath.Join("imports", uuid.NewString()+ext))
	if err := s.store.Put(c.Request.Context(), storageKey, bytes.NewReader(content)); err != nil {
		failure(c, http.StatusInternalServerError, "STORAGE_ERROR", "无法保存排班截图", nil)
		return
	}
	s.logger.DebugContext(c.Request.Context(), "image upload stored", "workspace_id", workspaceID, "storage_key", storageKey, "bytes", len(content))
	jobID, keep, err := s.persistAI(c, workspaceID, in, "IMAGE_AI", filepath.Base(header.Filename), storageKey, mediaType, content, snapshot)
	if !keep {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.Delete(cleanup, storageKey)
	}
	if err != nil {
		s.aiCreationError(c, err)
		return
	}
	var state string
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT state FROM import_jobs WHERE id=$1`, jobID).Scan(&state); err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取任务状态", nil)
		return
	}
	success(c, http.StatusAccepted, gin.H{"id": jobID, "state": state, "firstFrameOnly": imageFormat == "gif"})

}

// downloadImportFile 校验导入访问权限后按附件方式返回原始文件
func (s *server) downloadImportFile(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var storageKey, originalName, mediaType string
	if err := s.db.QueryRowContext(c.Request.Context(), `
SELECT f.storage_key, f.original_name, f.media_type FROM import_files f JOIN import_jobs j ON j.id = f.import_job_id
WHERE j.id = $1 AND j.workspace_id = $2`, jobID, workspaceID).Scan(&storageKey, &originalName, &mediaType); err != nil {
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

// cancelImport 将允许取消的导入任务置为取消状态并记录审计
func (s *server) cancelImport(c *gin.Context) {
	workspace, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法取消任务", nil)
		return
	}
	defer tx.Rollback()
	var state string
	var target, g uint64
	if tx.QueryRowContext(c.Request.Context(), `SELECT state,target_user_id,run_generation FROM import_jobs WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, job, workspace).Scan(&state, &target, &g) != nil {
		failure(c, 404, "IMPORT_NOT_FOUND", "任务不存在", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspace, target) {
		return
	}
	if state == "CANCELLED" {
		success(c, 200, gin.H{"id": job, "state": state})
		return
	}
	if state != "UPLOADED" && state != "PENDING" && state != "PARSING" && state != "NEEDS_REVIEW" {
		failure(c, 409, "IMPORT_NOT_CANCELLABLE", "当前任务不可取消", gin.H{"state": state})
		return
	}
	_, err = tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state='CANCELLED',stage='CANCELLED',lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=NULL WHERE id=$1`, job)
	if err == nil {
		_, err = tx.ExecContext(c.Request.Context(), `UPDATE import_provider_calls c SET finished_at=statement_timestamp(),code='CANCELLED' FROM import_attempts a WHERE a.id=c.attempt_id AND a.job_id=$1 AND a.generation=$2 AND a.state='RUNNING' AND c.finished_at IS NULL`, job, g)
	}
	if err == nil {
		_, err = tx.ExecContext(c.Request.Context(), `UPDATE import_attempts SET state='CANCELLED',finished_at=statement_timestamp() WHERE job_id=$1 AND generation=$2 AND state='RUNNING'`, job, g)
	}
	if err == nil {
		err = writeImportAudit(c.Request.Context(), tx, workspace, currentUserID(c), job, "IMPORT_CANCELLED")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法保存取消", nil)
		return
	}
	if s.importCancel != nil {
		s.importCancel(c.Request.Context(), job, g)
	}
	success(c, 200, gin.H{"id": job, "state": "CANCELLED"})
}

// downloadImportTemplate 读取工作区班次并生成带校验和说明的 XLSX 模板
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

// newScheduleImportTemplate 构造固定表头、示例行与输入校验，成功后由调用方关闭工作簿
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
	// 日期列按文本保存，避免电子表格软件将 ISO 日期转换为地区格式或序列值
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
	// 隐藏选项表提供状态、班次和跨日列表的数据源，不参与首表导入
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
	// 空班次名称与重复名称不进入下拉列表，无启用班次时提供提示占位
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
	// DataValidation 将 Formula1 写入 XML 文本，比较运算符必须预先转义
	formula := `=OR(AND($B2="",$C2="",$D2="",$E2="",$F2=""),AND($B2="休息",$C2="",$D2="",$E2="",OR($F2="",$F2="否")),AND($B2="工作",$C2&lt;&gt;"",$D2="",$E2=""),AND($B2="工作",$C2="",$D2="",$E2="",OR($F2="",$F2="否")),AND($B2="工作",$C2="",$D2&lt;&gt;"",$E2&lt;&gt;""))`
	if err := addTemplateCustomValidation(workbook, sheet, "B2:G1000", formula); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	return workbook, nil
}

// addTemplateListValidation 为指定单元格范围设置列表校验和输入提示
func addTemplateListValidation(workbook *excelize.File, sheet, sqref, source, title, prompt string) error {
	dv := excelize.NewDataValidation(true)
	dv.Sqref = sqref
	dv.SetSqrefDropList(source)
	dv.SetError(excelize.DataValidationErrorStyleStop, "输入无效", "shiftory 模板：请选择下拉选项")
	dv.SetInput(title, prompt)
	return workbook.AddDataValidation(sheet, dv)
}

// addTemplateCustomValidation 为指定单元格范围设置自定义公式校验
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

// enabledShiftNames 读取工作区启用班次的展示名称供模板选择
func (s *server) enabledShiftNames(ctx context.Context, workspaceID uint64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name FROM shifts WHERE workspace_id = $1 AND enabled = TRUE ORDER BY sort_order, id`, workspaceID)
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

// shiftMappings 加载启用班次快照与别名，供工作簿规范化使用
func (s *server) shiftMappings(ctx context.Context, workspaceID uint64) (map[string]importer.ShiftMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, code, to_char(start_time::interval, 'HH24:MI'), to_char(end_time::interval, 'HH24:MI'), cross_day, display_color
FROM shifts WHERE workspace_id = $1 AND enabled = TRUE`, workspaceID)
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
	aliasRows, err := s.db.QueryContext(ctx, `SELECT shift_id, alias FROM shift_aliases WHERE workspace_id = $1`, workspaceID)
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

// listImports 按工作区及上传者权限返回导入任务摘要
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
SELECT id, target_user_id, import_type, state, to_char(period_start, 'YYYY-MM-DD'), to_char(period_end, 'YYYY-MM-DD'),
       source_filename, item_count, conflict_count, invalid_count, created_at, completed_at, rolled_back_at
FROM import_jobs WHERE workspace_id = $1`
	args := []any{workspaceID}
	if c.Query("scope") == "mine" || !requireAdmin(member) {
		query += " AND upload_user_id = $2"
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

// rowScanner 统一单行查询与结果集的字段解码入口
type rowScanner interface{ Scan(...any) error }

// scanImportSummary 解码导入摘要查询列，按可空值填充响应字段
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

// getImport 校验导入访问范围并返回任务摘要及预览项
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
SELECT id, target_user_id, import_type, state, to_char(period_start, 'YYYY-MM-DD'), to_char(period_end, 'YYYY-MM-DD'),
       source_filename, item_count, conflict_count, invalid_count, created_at, completed_at, rolled_back_at
FROM import_jobs WHERE id = $1 AND workspace_id = $2`, jobID, workspaceID)
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
	var version, g uint64
	var stage, description, code, hint string
	var rounds, writes int
	var issues, rules []byte
	var next sql.NullTime
	err = s.db.QueryRowContext(c.Request.Context(), `SELECT review_version,run_generation,stage,COALESCE(description,''),COALESCE(error_code,''),COALESCE(error_message,''),attempt_count,write_count,job_issues,rule_snapshot,retry_not_before FROM import_jobs WHERE id=$1`, jobID).Scan(&version, &g, &stage, &description, &code, &hint, &rounds, &writes, &issues, &rules, &next)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取任务阶段", nil)
		return
	}
	state, _ := job["state"].(string)
	if state == "NEEDS_REVIEW" {
		stage = "REVIEW"
	}
	if state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" || state == "ROLLED_BACK" {
		stage = state
	}
	job["legacyResponseFormat"] = "normalized_draft"
	job["reviewVersion"], job["runGeneration"], job["stage"], job["description"], job["errorCode"], job["errorMessage"], job["round"], job["writeCount"] = version, g, stage, description, code, hint, rounds, writes
	if next.Valid {
		job["nextRetryAt"] = next.Time
	}
	if len(issues) > 0 {
		job["issues"] = json.RawMessage(issues)
	}
	if len(rules) > 0 {
		job["rules"] = json.RawMessage(rules)
	}

	kind, _ := job["importType"].(string)
	if !s.config.AIEnabled && (kind == "TEXT_AI" || kind == "IMAGE_AI") && (state == "PENDING" || state == "PARSING") {
		job["stage"] = "PAUSED"
	}
	member, err := s.membership(workspaceID, currentUserID(c))
	canWrite := err == nil && s.userBelongsToWorkspace(workspaceID, job["targetUserId"].(uint64)) && (requireAdmin(member) || job["targetUserId"].(uint64) == currentUserID(c))
	job["actions"] = gin.H{"review": canWrite && state == "NEEDS_REVIEW", "commit": canWrite && state == "NEEDS_REVIEW", "rollback": canWrite && state == "COMPLETED", "cancel": canWrite && (state == "PENDING" || state == "PARSING" || state == "NEEDS_REVIEW"), "retry": canWrite && s.config.AIEnabled && state == "FAILED" && (kind == "TEXT_AI" || kind == "IMAGE_AI"), "file": kind != "TEXT_AI"}

	job["items"] = importItemResponses(items)
	success(c, http.StatusOK, job)
}

// queryer 允许预览读取复用数据库连接池或调用方事务
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// loadImportItems 通过数据库或事务读取逐日草稿、版本锚点及人工决定
func (s *server) loadImportItems(ctx context.Context, db queryer, jobID uint64) ([]importItemRecord, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, to_char(work_date, 'YYYY-MM-DD'), item_type, draft_snapshot, existing_schedule_id, existing_version, COALESCE(decision, ''), issues, error_message
FROM import_items WHERE import_job_id = $1 ORDER BY sort_order, work_date`, jobID)
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

// importItemResponses 解码快照和问题 JSON，构造预览项响应
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

// updateImportDecisions 在事务中保存待审核项目的人工处理决定
func (s *server) updateImportDecisions(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var request struct {
		ExpectedReviewVersion uint64 `json:"expectedReviewVersion"`
		Decisions             []struct {
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
	var target uint64
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT state,target_user_id FROM import_jobs WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, jobID, workspaceID).Scan(&state, &target); err != nil || state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_REVIEWABLE", "导入任务当前不可修改", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspaceID, target) {
		return
	}
	if !s.requireReviewVersion(c, tx, jobID, request.ExpectedReviewVersion) {
		return
	}
	// 仅新增和冲突项接受显式决定，条目必须属于锁定的待审核任务
	for _, decision := range request.Decisions {
		if decision.Decision != "KEEP_EXISTING" && decision.Decision != "USE_IMPORTED" && decision.Decision != "SKIP" {
			failure(c, http.StatusBadRequest, "INVALID_DECISION", "冲突决策值无效", nil)
			return
		}
		result, err := tx.ExecContext(c.Request.Context(), `
UPDATE import_items SET decision = $1 WHERE id = $2 AND import_job_id = $3 AND (item_type IN ('CONFLICT', 'NEW') OR $4='SKIP')`, decision.Decision, decision.ItemID, jobID, decision.Decision)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存冲突决策", nil)
			return
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			failure(c, http.StatusBadRequest, "INVALID_IMPORT_ITEM", "导入条目不存在或无需决策", nil)
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET review_version=review_version+1 WHERE id=$1`, jobID); err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法更新审查版本", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交冲突决策", nil)
		return
	}
	success(c, http.StatusOK, gin.H{"updated": len(request.Decisions), "reviewVersion": request.ExpectedReviewVersion + 1})
}

// correctImportItem 使用校验通过的人工排班替换可复核草稿
// 当前排班版本会成为新的预览基线，后续版本变化仍由 commitImport 按过期预览拒绝
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
	var request struct {
		scheduleRequest
		ExpectedReviewVersion uint64 `json:"expectedReviewVersion"`
	}
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
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, import_type, state FROM import_jobs WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &importType, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_REVIEWABLE", "导入任务当前不可修改", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspaceID, targetUserID) {
		return
	}
	if !s.requireReviewVersion(c, tx, jobID, request.ExpectedReviewVersion) {
		return
	}
	var dateText string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT to_char(work_date, 'YYYY-MM-DD') FROM import_items WHERE id = $1 AND import_job_id = $2 FOR UPDATE`, itemID, jobID).Scan(&dateText); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_IMPORT_ITEM", "导入条目不存在", nil)
		return
	}
	date := schedule.MustDate(dateText)
	// 人工输入重新走班次快照与领域校验，不能直接将请求 JSON 作为可信草稿
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
	// 以当前锁定排班重新分类，保存新的版本基线并清除已失效的旧决定和问题
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
SET item_type = $1, draft_snapshot = $2, existing_schedule_id = $3, existing_version = $4, decision = NULL, issues = NULL, error_message = NULL
WHERE id = $5 AND import_job_id = $6`, itemType, draftJSON, existingID, existingVersion, itemID, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存预览修正", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE import_jobs
SET review_version=review_version+1,conflict_count = (SELECT COUNT(*) FROM import_items WHERE import_job_id = $1 AND item_type = 'CONFLICT'),
    invalid_count = (SELECT COUNT(*) FROM import_items WHERE import_job_id = $2 AND item_type IN ('INVALID', 'UNCERTAIN'))
WHERE id = $3`, jobID, jobID, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新导入统计", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交预览修正", nil)
		return
	}
	response := gin.H{"id": itemID, "workDate": date.String(), "type": itemType, "decision": "", "draft": draft, "reviewVersion": request.ExpectedReviewVersion + 1}
	if found {
		response["existingScheduleId"], response["existingVersion"] = existing.ID, existing.Version
	}
	success(c, http.StatusOK, response)
}

// parseImportScope 解析工作区和导入 ID，并检查当前用户的导入访问权限
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

// requireImportAccess 限制原始导入资料仅由工作区管理员或上传者访问
func (s *server) requireImportAccess(c *gin.Context, workspaceID, jobID uint64) bool {
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return false
	}
	var uploaderID uint64
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT upload_user_id FROM import_jobs WHERE id = $1 AND workspace_id = $2`, jobID, workspaceID).Scan(&uploaderID); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return false
	}
	if !requireAdmin(member) && uploaderID != currentUserID(c) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权查看或操作该导入任务", nil)
		return false
	}
	if c.Request.Method != http.MethodGet {
		var targetUserID uint64
		if err := s.db.QueryRowContext(c.Request.Context(), "SELECT target_user_id FROM import_jobs WHERE id = $1 AND workspace_id = $2", jobID, workspaceID).Scan(&targetUserID); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法检查导入目标", nil)
			return false
		}
		if !s.userBelongsToWorkspace(workspaceID, targetUserID) || (targetUserID != currentUserID(c) && !requireAdmin(member)) {
			failure(c, http.StatusForbidden, "FORBIDDEN", "无权修改目标成员的导入任务", nil)
			return false
		}
	}
	return true
}

// requireImportWriterTx 锁定操作者和目标成员，提交期间的权限撤销不能与排班写入交错
func (s *server) requireImportWriterTx(c *gin.Context, tx *sql.Tx, workspaceID, targetID uint64) bool {
	rows, err := tx.QueryContext(c.Request.Context(), "SELECT user_id, role, status FROM workspace_members WHERE workspace_id = $1 AND user_id IN ($2, $3) ORDER BY user_id FOR SHARE", workspaceID, currentUserID(c), targetID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法锁定导入权限", nil)
		return false
	}
	defer rows.Close()
	members := map[uint64]membership{}
	for rows.Next() {
		var member membership
		if err := rows.Scan(&member.UserID, &member.Role, &member.Status); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入权限", nil)
			return false
		}
		members[member.UserID] = member
	}
	if err := rows.Err(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入权限", nil)
		return false
	}
	actor, target := members[currentUserID(c)], members[targetID]
	if actor.Status != "ACTIVE" || target.Status != "ACTIVE" || (actor.UserID != targetID && !requireAdmin(actor)) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权修改目标成员的排班", nil)
		return false
	}
	return true
}

// commitImport 锁定导入任务与选中排班，重新校验预览版本后原子写入并完成任务
func (s *server) commitImport(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var request struct {
		ExpectedReviewVersion uint64 `json:"expectedReviewVersion"`
	}
	if c.ShouldBindJSON(&request) != nil {
		failure(c, 400, "INVALID_REQUEST", "请求无效", nil)
		return
	}
	// 提交会锁定任务及目标日期，并用预览时记录的排班 ID 和版本检测过期数据
	// 任一条目冲突都会回滚整个导入事务
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetUserID uint64
	var importType, state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, import_type, state FROM import_jobs WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &importType, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspaceID, targetUserID) {
		return
	}
	// 已完成任务直接返回，避免重复请求再次写入排班和修订
	if state == "COMPLETED" {
		success(c, http.StatusOK, gin.H{"id": jobID, "state": state})
		return
	}
	if state != "NEEDS_REVIEW" {
		failure(c, http.StatusConflict, "IMPORT_NOT_COMMITTABLE", "导入任务当前不可提交", nil)
		return
	}
	if !s.requireReviewVersion(c, tx, jobID, request.ExpectedReviewVersion) {
		return
	}
	items, err := s.loadImportItems(c.Request.Context(), tx, jobID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取导入预览", nil)
		return
	}
	writeCount, trustCount := 0, 0
	for _, item := range items {
		if item.Type == "SAME" || item.Type == "NEW" || item.Type == "CONFLICT" || item.Decision == "SKIP" {
			trustCount++
		}
	}
	if trustCount == 0 {
		failure(c, 409, "NO_TRUSTED_RESULT", "没有可信结果，请修正或明确跳过", nil)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	// 逐项执行人工选择，未处理的冲突阻止整次提交，不确定和缺失项不写入
	for _, item := range items {
		if item.Type == "CONFLICT" && item.Decision == "" {
			failure(c, http.StatusConflict, "UNRESOLVED_CONFLICTS", "仍有冲突尚未处理", gin.H{"itemId": item.ID})
			return
		}
		if (item.Type != "NEW" && item.Type != "SAME" && item.Type != "CONFLICT") || item.Decision == "SKIP" || item.Draft == nil {
			continue
		}
		// 锁定目标日期并比较预览锚点，原本不存在的日期也必须再次确认未被创建
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
		} else if !found || current.ID != *item.ExistingScheduleID || item.ExistingVersion == nil || current.Version != *item.ExistingVersion {
			failure(c, http.StatusConflict, "STALE_PREVIEW", "预览后排班已发生变化，请重新导入", gin.H{"workDate": item.Date.String()})
			return
		}
		if item.Type == "SAME" || item.Decision == "KEEP_EXISTING" {
			continue
		}
		// 保存导入前快照与版本，写入排班后记录对应修订以支持整批撤销
		var beforeJSON any
		var beforeVersion any
		if found {
			payload, _ := json.Marshal(storedFromDay(current))
			beforeJSON, beforeVersion = payload, current.Version
		}
		importID := jobID
		day := dayFromStored(*item.Draft, workspaceID, targetUserID, item.Date)
		day.SourceType, day.SourceImportID, day.CreatedBy = schedule.SourceType(importType), &importID, currentUserID(c)
		if err := day.Validate(); err != nil {
			failure(c, 400, "INVALID_SCHEDULE", "草稿不满足排班规则", gin.H{"workDate": item.Date.String()})
			return
		}
		saved, err := writeScheduleTx(c.Request.Context(), tx, day, current, found)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法写入导入排班", nil)
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, saved.ID, workspaceID, targetUserID, item.Date.String(), beforeVersion, saved.Version, beforeJSON, afterJSON, changeType, jobID, currentUserID(c)); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录导入版本", nil)
			return
		}
		writeCount++
	}
	// 排班和修订写入后才标记任务完成，与所有业务写入一起提交
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state = 'COMPLETED',write_count=$1, completed_at = statement_timestamp() WHERE id = $2`, writeCount, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法完成导入任务", nil)
		return
	}
	if err := writeImportAudit(c.Request.Context(), tx, workspaceID, currentUserID(c), jobID, "IMPORT_COMMIT"); err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法保存审计", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交导入事务", nil)
		return
	}

	success(c, http.StatusOK, gin.H{"id": jobID, "state": "COMPLETED", "writeCount": writeCount})
}

// rollbackImport 核对导入后的版本与来源，全量恢复原快照或删除该次创建的排班
func (s *server) rollbackImport(c *gin.Context) {
	workspaceID, jobID, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	// 回滚仅处理仍由本次导入拥有且版本未变化的排班，避免覆盖后续人工修改
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法回滚导入", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetUserID uint64
	var state string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT target_user_id, state FROM import_jobs WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, jobID, workspaceID).Scan(&targetUserID, &state); err != nil {
		failure(c, http.StatusNotFound, "IMPORT_NOT_FOUND", "导入任务不存在", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspaceID, targetUserID) {
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
	// 按修订倒序加载本次导入的写入结果，全部读完并关闭游标后再修改排班
	rows, err := tx.QueryContext(c.Request.Context(), `
SELECT to_char(work_date, 'YYYY-MM-DD'), before_snapshot, after_snapshot, after_version
FROM schedule_revisions WHERE import_job_id = $1 AND change_type IN ('IMPORT_CREATE', 'IMPORT_UPDATE') ORDER BY id DESC`, jobID)
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
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].date < revisions[j].date })
	// 逐日核对当前版本和导入来源，任一后续修改都会使整个撤销事务回滚
	for _, revision := range revisions {
		current, found, err := queryScheduleTx(c.Request.Context(), tx, workspaceID, targetUserID, revision.date, true)
		if err != nil || !found || current.Version != revision.afterVersion || current.SourceImportID == nil || *current.SourceImportID != jobID {
			failure(c, http.StatusConflict, "ROLLBACK_CONFLICT", "导入后的排班已被修改，不能自动回滚", gin.H{"workDate": revision.date.String()})
			return
		}
		// 没有前快照表示本次导入创建了排班，撤销时删除记录并写删除修订
		if len(revision.before) == 0 {
			if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM schedule_days WHERE id = $1`, current.ID); err != nil {
				failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法删除导入新增排班", nil)
				return
			}
			if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, import_job_id, changed_by)
VALUES (NULL, $1, $2, $3, $4, NULL, $5, NULL, 'IMPORT_ROLLBACK_DELETE', $6, $7)`, workspaceID, targetUserID, revision.date.String(), current.Version, revision.after, jobID, currentUserID(c)); err != nil {
				failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录回滚版本", nil)
				return
			}
			continue
		}
		// 有前快照时恢复原业务内容但继续递增版本，避免旧客户端版本重新有效
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'IMPORT_ROLLBACK_RESTORE', $9, $10)`, current.ID, workspaceID, targetUserID, revision.date.String(), current.Version, restored.Version, revision.after, restoredJSON, jobID, currentUserID(c)); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录回滚版本", nil)
			return
		}
	}
	// 所有目标日期处理成功后，与任务撤销状态一并提交事务
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state = 'ROLLED_BACK', rolled_back_at = statement_timestamp() WHERE id = $1`, jobID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法完成回滚", nil)
		return
	}
	if err := writeImportAudit(c.Request.Context(), tx, workspaceID, currentUserID(c), jobID, "IMPORT_ROLLBACK"); err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法保存审计", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交回滚事务", nil)
		return
	}

	success(c, http.StatusOK, gin.H{"id": jobID, "state": "ROLLED_BACK"})
}

// storedFromEntry 提取导入草稿的排班内容用于持久化快照
func storedFromEntry(entry importer.Entry) storedSchedule {
	day := schedule.Day{Status: entry.Status, Note: entry.Note, Segments: entry.Segments}
	return storedFromDay(day)
}

// storedFromDay 提取正式排班内容用于比较和修订快照
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

// dayFromStored 将快照还原为指定用户、日期和工作区上下文中的领域排班
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

// schedulesEquivalent 按规范化后的业务内容比较排班，忽略来源与记录元数据
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

// equalUintPtr 比较可空整数指针的值，区分空值与零值
func equalUintPtr(left, right *uint64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

// queryScheduleTx 在事务中按用户与日期读取全局排班，lock 控制是否加行锁
func queryScheduleTx(ctx context.Context, tx *sql.Tx, workspaceID, userID uint64, date schedule.Date, lock bool) (schedule.Day, bool, error) {
	// 排班按用户和日期全局唯一，workspaceID 仅保留当前请求及版本记录的工作区上下文
	query := `
SELECT id, status, source_type, source_import_id, note, version, created_by
FROM schedule_days WHERE user_id = $1 AND work_date = $2`
	if lock {
		query += " FOR UPDATE"
	}
	day := schedule.Day{WorkspaceID: workspaceID, UserID: userID, WorkDate: date}
	var importID sql.NullInt64
	err := tx.QueryRowContext(ctx, query, userID, date.String()).Scan(&day.ID, &day.Status, &day.SourceType, &importID, &day.Note, &day.Version, &day.CreatedBy)
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

// querySegmentsTx 在调用方事务内读取排班的全部时间段
func querySegmentsTx(ctx context.Context, tx *sql.Tx, dayID uint64) ([]schedule.Segment, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, segment_type, shift_id, COALESCE(shift_name_snapshot, ''), COALESCE(shift_code_snapshot, ''),
       to_char(start_time::interval, 'HH24:MI'), to_char(end_time::interval, 'HH24:MI'), cross_day,
       COALESCE(display_color_snapshot, ''), sort_order, COALESCE(original_label, '')
FROM schedule_segments WHERE schedule_day_id = $1 ORDER BY sort_order, id`, dayID)
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

// writeScheduleTx 在调用方事务中创建或更新排班及时间段，事务提交由调用方负责
func writeScheduleTx(ctx context.Context, tx *sql.Tx, day, existing schedule.Day, found bool) (schedule.Day, error) {
	// 历史快照可引用已经解散的来源，恢复业务内容时仅保留仍存在的外键
	if day.SourceImportID != nil {
		var id uint64
		err := tx.QueryRowContext(ctx, "SELECT id FROM import_jobs WHERE id = $1 FOR SHARE", *day.SourceImportID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			day.SourceImportID = nil
		} else if err != nil {
			return schedule.Day{}, err
		}
	}
	for index := range day.Segments {
		if day.Segments[index].ShiftID == nil {
			continue
		}
		var id uint64
		err := tx.QueryRowContext(ctx, "SELECT id FROM shifts WHERE id = $1 FOR SHARE", *day.Segments[index].ShiftID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			day.Segments[index].ShiftID = nil
		} else if err != nil {
			return schedule.Day{}, err
		}
	}

	if found {
		day.ID, day.Version = existing.ID, existing.Version+1
		_, err := tx.ExecContext(ctx, `
UPDATE schedule_days SET status = $1, source_type = $2, source_import_id = $3, note = $4, version = $5 WHERE id = $6 AND version = $7`,
			day.Status, day.SourceType, day.SourceImportID, day.Note, day.Version, day.ID, existing.Version)
		if err != nil {
			return schedule.Day{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_segments WHERE schedule_day_id = $1`, day.ID); err != nil {
			return schedule.Day{}, err
		}
	} else {
		day.Version = 1
		var id int64
		err := tx.QueryRowContext(ctx, `
INSERT INTO schedule_days (workspace_id, user_id, work_date, status, source_type, source_import_id, note, version, created_by)
VALUES (NULLIF($1::bigint, 0), $2, $3, $4, $5, $6, $7, 1, $8) RETURNING id`, day.WorkspaceID, day.UserID, day.WorkDate.String(), day.Status, day.SourceType, day.SourceImportID, day.Note, day.CreatedBy).Scan(&id)
		if err != nil {
			return schedule.Day{}, err
		}

		day.ID = uint64(id)
	}
	for index, segment := range day.Segments {
		var start, end any
		if segment.StartTime != nil {
			start, end = segment.StartTime.String()+":00", segment.EndTime.String()+":00"
		}
		var segmentID int64
		err := tx.QueryRowContext(ctx, `
INSERT INTO schedule_segments
    (schedule_day_id, segment_type, shift_id, shift_name_snapshot, shift_code_snapshot, start_time, end_time, cross_day, display_color_snapshot, sort_order, original_label)
VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, NULLIF($9, ''), $10, NULLIF($11, '')) RETURNING id`, day.ID, segment.Type, segment.ShiftID,
			segment.ShiftName, segment.ShiftCode, start, end, segment.CrossDay, segment.DisplayColor, segment.SortOrder, segment.OriginalLabel).Scan(&segmentID)
		if err != nil {
			return schedule.Day{}, err
		}

		if err != nil {
			return schedule.Day{}, err
		}
		day.Segments[index].ID = uint64(segmentID)
	}
	return day, nil
}
