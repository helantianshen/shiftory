package importjob

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"shiftory-server/internal/ai"
	"shiftory-server/internal/importer/imageai"
	"shiftory-server/internal/platform/storage"
	"shiftory-server/internal/schedule"
)

const maxImageBytes = int64(10 << 20)

// ImageRecognizer 将单成员图片与用户指定边界转换为结构化草稿
type ImageRecognizer interface {
	// Recognize 在指定上下文内识别图片并返回结构化草稿
	Recognize(context.Context, imageai.Request) (imageai.Draft, error)
}

// ImageProcessor 组合文件、模型和班次查询以生成图片任务预览
type ImageProcessor struct {
	db         *sql.DB
	store      storage.Store
	recognizer ImageRecognizer
	modelName  string
	logger     *slog.Logger
}

// NewImageProcessor 以默认日志器组合数据库、文件存储和图片识别器
func NewImageProcessor(db *sql.DB, store storage.Store, recognizer ImageRecognizer, modelName string) *ImageProcessor {
	return NewImageProcessorWithLogger(db, store, recognizer, modelName, slog.Default())
}

// NewImageProcessorWithLogger 配置图片草稿处理依赖，不在构造时启动任务
func NewImageProcessorWithLogger(db *sql.DB, store storage.Store, recognizer ImageRecognizer, modelName string, logger *slog.Logger) *ImageProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ImageProcessor{db: db, store: store, recognizer: recognizer, modelName: modelName, logger: logger}
}

// Process 读取图片并识别，按请求区间生成缺失、冲突或待审核草稿
func (p *ImageProcessor) Process(ctx context.Context, job Job) (Result, error) {
	if p == nil || p.db == nil || p.store == nil || p.recognizer == nil {
		return Result{}, errors.New("image processor is not configured")
	}
	if job.AttemptID > 0 {
		if r, ok := p.recognizer.(interface{ Fingerprint() string }); ok {
			version := imageai.SchemaVersion
			if job.Description != "" {
				version = imageai.RuleSchemaVersion
			}
			if _, err := p.db.ExecContext(ctx, `UPDATE import_attempts SET config_fingerprint=$1,prompt_version=$2,schema_version=$3 WHERE id=$4`, r.Fingerprint(), imageai.PromptVersion, version, job.AttemptID); err != nil {
				return Result{}, errors.New("attempt diagnostic persistence failed")
			}
		}
	}

	p.logger.Debug("image recognition processing started", "job_id", job.ID, "attempt", job.AttemptCount, "workspace_id", job.WorkspaceID, "target_user_id", job.TargetUserID)
	// 读取持久化原文件并再次校验大小和像素数，防止无效输入进入模型
	var content []byte
	var format string
	if job.ImportType != "TEXT_AI" {
		reader, err := p.store.Open(ctx, job.StorageKey)
		if err != nil {
			return Result{}, fmt.Errorf("open image: %w", err)
		}
		defer reader.Close()
		content, err = io.ReadAll(io.LimitReader(reader, maxImageBytes+1))
		if err != nil || int64(len(content)) > maxImageBytes {
			return Result{}, errors.New("image exceeds size limit or cannot be read")
		}
		var config image.Config
		config, format, err = image.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 25_000_000 {
			return Result{}, errors.New("image is invalid or exceeds 25 megapixels")
		}
		p.logger.Debug("image recognition input validated", "job_id", job.ID, "bytes", len(content), "width", config.Width, "height", config.Height, "format", format)
	}
	// 从固定班次快照建立映射，提示必须对应快照中的唯一班次
	var aliases, shifts map[string]processorShift
	var input frozenInput
	var err error
	if len(job.InputSnapshot) > 0 {
		if err = json.Unmarshal(job.InputSnapshot, &input); err != nil {
			return Result{}, errors.New("invalid input snapshot")
		}
		aliases, shifts = input.Aliases, input.Shifts
	} else {
		aliases, shifts, err = p.loadShiftMappings(ctx, job.WorkspaceID)
	}
	if err != nil {
		return Result{}, err
	}
	providedMappings := make(map[string]string, len(aliases)+len(job.MappingHints))
	for alias, shift := range aliases {
		providedMappings[alias] = shift.Code
	}
	for alias, code := range job.MappingHints {
		key := strings.ToLower(strings.TrimSpace(alias))
		shift, ok := shifts[strings.ToLower(strings.TrimSpace(code))]
		if !ok {
			for _, candidate := range shifts {
				if strings.EqualFold(candidate.Name, code) {
					shift, ok = candidate, true
					break
				}
			}
		}
		if !ok {
			return Result{}, errors.New("mapping hint unavailable in input snapshot")
		}
		if existing, found := aliases[key]; found && existing.ID != shift.ID {
			return Result{}, errors.New("mapping hint conflicts with input snapshot")
		}
		aliases[key] = shift
		providedMappings[key] = shift.Code
	}
	p.logger.Info("AI recognition started", "job_id", job.ID, "period_start", job.PeriodStart.String(), "period_end", job.PeriodEnd.String(), "mapping_count", len(providedMappings))
	if len(content) > 0 {
		content, format, err = imageai.PrepareImage(content)
		if err != nil {
			return Result{}, err
		}
	}
	sequence := 0
	var callID int64
	recorder := func(c context.Context, call imageai.Call) error {
		if call.FinishedAt.IsZero() {
			p.logger.InfoContext(c, "AI provider selected", "job_id", job.ID, "attempt_id", job.AttemptID, "generation", job.Generation, "provider_id", call.ProviderID, "model", call.Model)
		} else {
			p.logger.InfoContext(c, "AI provider call finished", "job_id", job.ID, "attempt_id", job.AttemptID, "generation", job.Generation, "provider_id", call.ProviderID, "model", call.Model, "code", call.Code, "http_status", call.HTTPStatus, "duration_ms", call.FinishedAt.Sub(call.StartedAt).Milliseconds())
		}
		if job.AttemptID == 0 {
			return nil
		}
		if call.FinishedAt.IsZero() {
			sequence++
			e := p.db.QueryRowContext(c, `INSERT INTO import_provider_calls(attempt_id,provider_id,model,sequence,started_at) SELECT $1,$2,$3,$4,$5 FROM import_jobs WHERE id=$6 AND state='PARSING' AND run_generation=$7 AND lease_owner=$8 AND lease_expires_at>statement_timestamp() RETURNING id`, job.AttemptID, call.ProviderID, call.Model, sequence, call.StartedAt, job.ID, job.Generation, job.LeaseOwner).Scan(&callID)
			if errors.Is(e, sql.ErrNoRows) {
				return errors.New("execution ownership lost")
			}
			if e != nil {
				return errors.New("call diagnostic persistence failed")
			}
			return nil
		}
		res, e := p.db.ExecContext(c, `UPDATE import_provider_calls SET finished_at=$1,http_status=$2,code=$3,raw_text=$4,normalized=$5,finish_reason=$6,output_tokens=$7 WHERE id=$8 AND EXISTS(SELECT 1 FROM import_jobs WHERE id=$9 AND state='PARSING' AND run_generation=$10 AND lease_owner=$11 AND lease_expires_at>statement_timestamp())`, call.FinishedAt, call.HTTPStatus, call.Code, call.Raw, nullableJSON(call.Normalized), call.FinishReason, call.OutputTokens, callID, job.ID, job.Generation, job.LeaseOwner)
		if e != nil {
			return errors.New("call diagnostic persistence failed")
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("execution ownership lost")
		}
		return nil
	}
	draft, err := p.recognizer.Recognize(ctx, imageai.Request{Description: job.Description, Timezone: input.Timezone, FixedNow: input.FixedNow, OnCall: recorder, Image: content, ImageFormat: format, Start: job.PeriodStart, End: job.PeriodEnd,
		Instructions: job.RecognitionInstructions, ShiftMappings: providedMappings})
	if err != nil {
		var pe *ai.ProcessingError
		code := "AI_PROCESSING_FAILED"
		if errors.As(err, &pe) {
			code = pe.Code
		}
		p.logger.Error("AI request failed", "job_id", job.ID, "code", code)
		if errors.As(err, &pe) && pe.Retryable {
			return Result{}, Retryable(err)
		}
		return Result{}, err
	}
	p.logger.Info("AI recognition succeeded", "job_id", job.ID, "provider_id", draft.ProviderID, "model", draft.ModelName, "entry_count", len(draft.Entries))
	if err := draft.Validate(job.PeriodStart, job.PeriodEnd); err != nil {
		return Result{}, fmt.Errorf("validate AI draft: %w", err)
	}
	// 保存识别器返回草稿的 JSON 快照，按日期索引后生成完整区间预览
	raw, _ := json.Marshal(draft)
	entries := make(map[schedule.Date]imageai.Entry, len(draft.Entries))
	for _, entry := range draft.Entries {
		entries[schedule.MustDate(entry.Date)] = entry
	}
	// 预览必须覆盖请求区间中的每一天，模型未返回的日期显式标记为 MISSING
	dates, err := datesBetween(job.PeriodStart, job.PeriodEnd)
	if err != nil || len(dates) > 366 {
		return Result{}, errors.New("image import period must not exceed 366 days")
	}
	result := Result{Items: make([]ResultItem, 0, len(dates)), ItemCount: len(dates), ModelName: p.modelName,
		PromptVersion: imageai.PromptVersion, SchemaVersion: imageai.SchemaVersion, RawResponse: raw, RulesSnapshot: draft.RulesSnapshot}
	if job.ImportType == "TEXT_AI" {
		result.SchemaVersion = imageai.RuleSchemaVersion
	}
	result.JobIssues, _ = json.Marshal(draft.Issues)
	if draft.ModelName != "" {
		result.ModelName = draft.ModelName
	}
	if job.Description != "" {
		result.SchemaVersion = imageai.RuleSchemaVersion
	}
	if job.AttemptID > 0 {
		if _, err := p.db.ExecContext(ctx, `UPDATE import_attempts SET config_fingerprint=$1,prompt_version=$2,schema_version=$3 WHERE id=$4`, draft.ConfigFingerprint, result.PromptVersion, result.SchemaVersion, job.AttemptID); err != nil {
			return Result{}, errors.New("attempt diagnostic persistence failed")
		}
	}
	for index, date := range dates {
		entry, found := entries[date]
		if !found {
			issueJSON, _ := json.Marshal(draft.Issues)
			result.Items = append(result.Items, ResultItem{Date: date, Type: "MISSING", Issues: issueJSON, SortOrder: index})
			continue
		}
		snapshot, issues := p.convertEntry(entry, aliases, shifts)
		issues = append(issues, entry.Issues...)
		issues = append(issues, draft.Issues...)
		itemType := "NEW"
		if entry.Uncertain || len(issues) > 0 {
			itemType = "UNCERTAIN"
		}
		for _, issue := range issues {
			if issue.Field == "day" {
				itemType = "INVALID"
			}
		}
		var draftJSON []byte
		if snapshot != nil {
			draftJSON, _ = json.Marshal(snapshot)
		}
		issuesJSON, _ := json.Marshal(issues)
		item := ResultItem{Date: date, Type: itemType, DraftSnapshot: draftJSON, Issues: issuesJSON, SortOrder: index}
		// 记录现有排班 ID 与版本，可信草稿据此区分一致和冲突，不确定项保持待修正
		existing, existingID, existingVersion, exists, err := p.loadExisting(ctx, job.WorkspaceID, job.TargetUserID, date)
		if err != nil {
			return Result{}, err
		}
		if exists {
			item.ExistingScheduleID, item.ExistingVersion = &existingID, &existingVersion
			if itemType != "UNCERTAIN" && itemType != "INVALID" && equivalentProcessorSnapshots(*snapshot, existing) {
				item.Type = "SAME"
			} else if itemType != "UNCERTAIN" && itemType != "INVALID" {
				item.Type = "CONFLICT"
				result.ConflictCount++
			}
		}
		if item.Type == "UNCERTAIN" || item.Type == "INVALID" {
			result.InvalidCount++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// processorShift 保存图片识别映射使用的启用班次信息
type processorShift struct {
	ID           uint64
	Name         string
	Code         string
	StartTime    string
	EndTime      string
	CrossDay     bool
	DisplayColor string
}

// processorSnapshot 保存图片预览用于比较的状态、备注和时间段
type processorSnapshot struct {
	RuleIDs  []string                   `json:"ruleIds,omitempty"`
	Status   schedule.Status            `json:"status"`
	Note     string                     `json:"note"`
	Segments []processorSegmentSnapshot `json:"segments"`
}

// processorSegmentSnapshot 保存图片草稿映射后的班次与时间信息
type processorSegmentSnapshot struct {
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

// loadShiftMappings 加载工作区启用班次，分别建立代码与别名映射
func (p *ImageProcessor) loadShiftMappings(ctx context.Context, workspaceID uint64) (map[string]processorShift, map[string]processorShift, error) {
	rows, err := p.db.QueryContext(ctx, `
SELECT id, name, code, COALESCE(to_char(start_time::interval, 'HH24:MI'), ''), COALESCE(to_char(end_time::interval, 'HH24:MI'), ''), cross_day, display_color
FROM shifts WHERE workspace_id = $1 AND enabled = TRUE`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	aliases, shifts := map[string]processorShift{}, map[string]processorShift{}
	for rows.Next() {
		var shift processorShift
		if err := rows.Scan(&shift.ID, &shift.Name, &shift.Code, &shift.StartTime, &shift.EndTime, &shift.CrossDay, &shift.DisplayColor); err != nil {
			return nil, nil, err
		}
		shifts[strings.ToLower(shift.Code)] = shift
		for _, value := range []string{shift.Code, shift.Name} {
			key := strings.ToLower(strings.TrimSpace(value))
			if existing, ok := aliases[key]; ok && existing.ID != shift.ID {
				return nil, nil, errors.New("ambiguous shift mapping")
			}
			aliases[key] = shift
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	aliasRows, err := p.db.QueryContext(ctx, `SELECT a.alias, s.id, s.name, s.code, COALESCE(to_char(s.start_time::interval, 'HH24:MI'), ''), COALESCE(to_char(s.end_time::interval, 'HH24:MI'), ''), s.cross_day, s.display_color FROM shift_aliases a JOIN shifts s ON s.id = a.shift_id WHERE a.workspace_id = $1 AND s.enabled = TRUE`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var alias string
		var shift processorShift
		if err := aliasRows.Scan(&alias, &shift.ID, &shift.Name, &shift.Code, &shift.StartTime, &shift.EndTime, &shift.CrossDay, &shift.DisplayColor); err != nil {
			return nil, nil, err
		}
		key := strings.ToLower(strings.TrimSpace(alias))
		if existing, ok := aliases[key]; ok && existing.ID != shift.ID {
			return nil, nil, errors.New("ambiguous shift alias")
		}
		aliases[key] = shift
	}
	return aliases, shifts, aliasRows.Err()
}

// convertEntry 将模型条目映射为班次快照，并返回需要人工处理的问题
func (p *ImageProcessor) convertEntry(entry imageai.Entry, aliases, shifts map[string]processorShift) (*processorSnapshot, []imageai.Issue) {
	snapshot := &processorSnapshot{RuleIDs: entry.RuleIDs, Status: schedule.Status(entry.Status), Note: strings.TrimSpace(entry.Note), Segments: make([]processorSegmentSnapshot, 0, len(entry.Segments))}
	issues := make([]imageai.Issue, 0)
	for index, item := range entry.Segments {
		segment := processorSegmentSnapshot{Type: schedule.SegmentType(item.Type), StartTime: item.StartTime, EndTime: item.EndTime, CrossDay: item.CrossDay, SortOrder: index, OriginalLabel: item.OriginalLabel}
		if segment.Type == schedule.SegmentShift {
			shift, found := shifts[strings.ToLower(strings.TrimSpace(item.MappedShiftCode))]
			if !found {
				shift, found = aliases[strings.ToLower(strings.TrimSpace(item.OriginalLabel))]
			}
			if !found {
				issues = append(issues, imageai.Issue{Field: fmt.Sprintf("segments[%d].mappedShiftCode", index), Message: "班次无法映射到当前工作区"})
			} else {
				segment.ShiftID, segment.ShiftName, segment.ShiftCode = &shift.ID, shift.Name, shift.Code
				segment.StartTime, segment.EndTime, segment.CrossDay, segment.DisplayColor = shift.StartTime, shift.EndTime, shift.CrossDay, shift.DisplayColor
			}
		}
		snapshot.Segments = append(snapshot.Segments, segment)
	}
	day := schedule.Day{WorkDate: schedule.Date(entry.Date), Status: snapshot.Status}
	for _, part := range snapshot.Segments {
		seg := schedule.Segment{Type: part.Type, ShiftID: part.ShiftID, ShiftName: part.ShiftName, CrossDay: part.CrossDay}
		if part.StartTime != "" {
			v, e := schedule.ParseClock(part.StartTime)
			if e != nil {
				issues = append(issues, imageai.Issue{Field: "segments", Message: "开始时间无效"})
			} else {
				seg.StartTime = &v
			}
		}
		if part.EndTime != "" {
			v, e := schedule.ParseClock(part.EndTime)
			if e != nil {
				issues = append(issues, imageai.Issue{Field: "segments", Message: "结束时间无效"})
			} else {
				seg.EndTime = &v
			}
		}
		day.Segments = append(day.Segments, seg)
	}
	if err := day.Validate(); err != nil && !errors.Is(err, schedule.ErrShiftSnapshotRequired) {
		issues = append(issues, imageai.Issue{Field: "day", Message: "排班领域校验失败：" + err.Error()})
	}
	return snapshot, issues
}

// loadExisting 按工作区、用户与日期读取现有排班快照和版本
func (p *ImageProcessor) loadExisting(ctx context.Context, workspaceID, userID uint64, date schedule.Date) (processorSnapshot, uint64, uint64, bool, error) {
	var snapshot processorSnapshot
	var id, version uint64
	err := p.db.QueryRowContext(ctx, `SELECT id, status, note, version FROM schedule_days WHERE user_id = $1 AND work_date = $2`, userID, date.String()).Scan(&id, &snapshot.Status, &snapshot.Note, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return processorSnapshot{}, 0, 0, false, nil
	}
	if err != nil {
		return processorSnapshot{}, 0, 0, false, err
	}
	rows, err := p.db.QueryContext(ctx, `
SELECT segment_type, shift_id, COALESCE(shift_name_snapshot, ''), COALESCE(shift_code_snapshot, ''), COALESCE(to_char(start_time::interval, 'HH24:MI'), ''),
       COALESCE(to_char(end_time::interval, 'HH24:MI'), ''), cross_day, COALESCE(display_color_snapshot, ''), sort_order, COALESCE(original_label, '')
FROM schedule_segments WHERE schedule_day_id = $1 ORDER BY sort_order, id`, id)
	if err != nil {
		return processorSnapshot{}, 0, 0, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var segment processorSegmentSnapshot
		var shiftID sql.NullInt64
		if err := rows.Scan(&segment.Type, &shiftID, &segment.ShiftName, &segment.ShiftCode, &segment.StartTime, &segment.EndTime, &segment.CrossDay, &segment.DisplayColor, &segment.SortOrder, &segment.OriginalLabel); err != nil {
			return processorSnapshot{}, 0, 0, false, err
		}
		if shiftID.Valid {
			value := uint64(shiftID.Int64)
			segment.ShiftID = &value
		}
		snapshot.Segments = append(snapshot.Segments, segment)
	}
	return snapshot, id, version, true, rows.Err()
}

// equivalentProcessorSnapshots 比较规范化排班内容，不比较来源与数据库记录标识
func equivalentProcessorSnapshots(left, right processorSnapshot) bool {
	if left.Status != right.Status || strings.TrimSpace(left.Note) != strings.TrimSpace(right.Note) || len(left.Segments) != len(right.Segments) {
		return false
	}
	for index := range left.Segments {
		a, b := left.Segments[index], right.Segments[index]
		if a.Type != b.Type || !equalProcessorIDs(a.ShiftID, b.ShiftID) || a.StartTime != b.StartTime || a.EndTime != b.EndTime || a.CrossDay != b.CrossDay {
			return false
		}
	}
	return true
}

// equalProcessorIDs 比较可空班次 ID，区分缺失与具体值
func equalProcessorIDs(left, right *uint64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

// datesBetween 展开包含首尾的日期区间，拒绝非法日期或倒置区间
func datesBetween(start, end schedule.Date) ([]schedule.Date, error) {
	current, err := time.Parse("2006-01-02", start.String())
	if err != nil {
		return nil, err
	}
	last, err := time.Parse("2006-01-02", end.String())
	if err != nil || last.Before(current) {
		return nil, errors.New("invalid date range")
	}
	dates := make([]schedule.Date, 0, int(last.Sub(current).Hours()/24)+1)
	for !current.After(last) {
		dates = append(dates, schedule.MustDate(current.Format("2006-01-02")))
		current = current.AddDate(0, 0, 1)
	}
	return dates, nil
}

type frozenInput struct {
	Timezone string                    `json:"timezone"`
	FixedNow time.Time                 `json:"fixedNow"`
	Aliases  map[string]processorShift `json:"aliases"`
	Shifts   map[string]processorShift `json:"shifts"`
}

func FreezeInput(ctx context.Context, db *sql.DB, workspaceID uint64) ([]byte, error) {
	p := &ImageProcessor{db: db}
	aliases, shifts, err := p.loadShiftMappings(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var zone string
	if err = db.QueryRowContext(ctx, `SELECT timezone FROM workspaces WHERE id=$1`, workspaceID).Scan(&zone); err != nil {
		return nil, err
	}
	if _, err = time.LoadLocation(zone); err != nil {
		return nil, errors.New("invalid workspace timezone")
	}
	return json.Marshal(frozenInput{zone, time.Now().UTC(), aliases, shifts})
}
func nullableJSON(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	return data
}
