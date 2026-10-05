package httpserver

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"shiftory-server/internal/importjob"
	"shiftory-server/internal/schedule"
)

type aiImportInput struct {
	TargetUserID    uint64            `json:"targetUserId"`
	PeriodStart     string            `json:"periodStart"`
	PeriodEnd       string            `json:"periodEnd"`
	Description     string            `json:"description"`
	Instructions    string            `json:"instructions"`
	MappingHints    map[string]string `json:"mappingHints"`
	RelatedImportID *uint64           `json:"relatedImportId,omitempty"`
}

func (s *server) createTextImport(c *gin.Context) {
	if !s.config.AI.Enabled {
		failure(c, 503, "AI_DISABLED", "AI 导入未启用", nil)
		return
	}
	if len(s.config.AI.Providers) > 0 && !s.config.AI.Supports(false) {
		failure(c, 503, "NO_AVAILABLE_PROVIDER", "没有配置支持文字的启用模型", nil)
		return
	}
	workspace, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	var in aiImportInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Description) == "" || len([]rune(in.Description)) > 10000 {
		failure(c, 400, "INVALID_DESCRIPTION", "请输入不超过 10000 字的排班描述", nil)
		return
	}
	in.Description = strings.TrimSpace(in.Description)
	if !s.validateAIInput(c, workspace, in) {
		return
	}
	if in.RelatedImportID != nil && !s.requireImportAccess(c, workspace, *in.RelatedImportID) {
		return
	}
	snapshot, err := importjob.FreezeInput(c.Request.Context(), s.db, workspace)
	if err != nil {
		failure(c, 500, "SNAPSHOT_ERROR", "无法固定识别输入", nil)
		return
	}
	job, _, err := s.persistAI(c, workspace, in, "TEXT_AI", "文字排班描述", "", "", nil, snapshot)
	if err != nil {
		s.aiCreationError(c, err)
		return
	}
	var state string
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT state FROM import_jobs WHERE id=$1`, job).Scan(&state); err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取任务状态", nil)
		return
	}
	success(c, http.StatusAccepted, gin.H{"id": job, "state": state})
}
func (s *server) validateAIInput(c *gin.Context, workspace uint64, in aiImportInput) bool {
	member, err := s.membership(workspace, currentUserID(c))
	if err != nil {
		failure(c, 403, "FORBIDDEN", "无权访问工作区", nil)
		return false
	}
	if in.TargetUserID == 0 || !s.userBelongsToWorkspace(workspace, in.TargetUserID) {
		failure(c, 400, "INVALID_MEMBER", "导入目标成员无效", nil)
		return false
	}
	if in.TargetUserID != currentUserID(c) && !requireAdmin(member) {
		failure(c, 403, "FORBIDDEN", "不能为其他成员导入排班", nil)
		return false
	}
	a, e := schedule.ParseDate(in.PeriodStart)
	b, f := schedule.ParseDate(in.PeriodEnd)
	if e != nil || f != nil {
		failure(c, 400, "INVALID_RANGE", "日期范围无效", nil)
		return false
	}
	dates, e := importDates(a, b)
	if e != nil || len(dates) > 366 {
		failure(c, 400, "INVALID_RANGE", "日期范围无效或超过 366 天", nil)
		return false
	}
	if len([]rune(in.Instructions)) > 2000 || len(in.MappingHints) > 100 {
		failure(c, 400, "INVALID_MAPPING_HINTS", "识别说明或映射提示超过限制", nil)
		return false
	}
	shifts, e := s.shiftMappings(c.Request.Context(), workspace)
	if e != nil {
		failure(c, 500, "DATABASE_ERROR", "无法校验班次映射", nil)
		return false
	}
	for alias, code := range in.MappingHints {
		if strings.TrimSpace(alias) == "" || len([]rune(alias)) > 128 || len([]rune(code)) > 64 {
			failure(c, 400, "INVALID_MAPPING_HINTS", "班次提示无效", nil)
			return false
		}
		found := 0
		seen := map[uint64]bool{}
		for _, v := range shifts {
			if (strings.EqualFold(v.Code, code) || strings.EqualFold(v.Name, code)) && !seen[v.ID] {
				found++
				seen[v.ID] = true
			}
		}
		if found != 1 {
			failure(c, 400, "INVALID_MAPPING_HINTS", "班次提示必须唯一对应当前工作区的启用班次", nil)
			return false
		}
	}
	return true
}

var errIdempotency = errors.New("idempotency conflict")

func (s *server) aiCreationError(c *gin.Context, err error) {
	if c.Writer.Written() {
		return
	}
	if errors.Is(err, errIdempotency) {
		failure(c, 409, "IDEMPOTENCY_CONFLICT", "同一请求键对应不同输入", nil)
	} else {
		failure(c, 500, "DATABASE_ERROR", "无法创建识别任务", nil)
	}
}
func (s *server) persistAI(c *gin.Context, workspace uint64, in aiImportInput, kind, filename, key, mime string, content, snapshot []byte) (uint64, bool, error) {
	idem := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idem == "" {
		idem = uuid.NewString()
	}
	if len(idem) > 128 {
		return 0, false, errIdempotency
	}
	digest := sha256.Sum256(content)
	fingerprintBytes, _ := json.Marshal(struct {
		Workspace, Uploader uint64
		Kind                string
		Input               aiImportInput
		Content             string
	}{workspace, currentUserID(c), kind, in, hex.EncodeToString(digest[:])})
	fingerprint := sha256.Sum256(fingerprintBytes)
	fp := hex.EncodeToString(fingerprint[:])
	existing := func(ctx context.Context) (uint64, error) {
		var id uint64
		var prior string
		var uploader uint64
		e := s.db.QueryRowContext(ctx, `SELECT id,input_fingerprint,upload_user_id FROM import_jobs WHERE workspace_id=$1 AND idempotency_key=$2`, workspace, idem).Scan(&id, &prior, &uploader)
		if e == nil && (prior != fp || uploader != currentUserID(c)) {
			return 0, errIdempotency
		}
		return id, e
	}
	if id, e := existing(c.Request.Context()); e == nil {
		return id, false, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return 0, false, e
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	if !s.requireImportWriterTx(c, tx, workspace, in.TargetUserID) {
		return 0, false, errors.New("authorization changed")
	}
	mappings, _ := json.Marshal(in.MappingHints)
	if in.MappingHints == nil {
		mappings = []byte(`{}`)
	}
	var id int64
	err = tx.QueryRowContext(c.Request.Context(), `INSERT INTO import_jobs(workspace_id,upload_user_id,target_user_id,import_type,state,period_start,period_end,source_filename,idempotency_key,input_fingerprint,recognition_instructions,mapping_hints,description,input_snapshot,max_attempts,related_import_id) VALUES($1,$2,$3,$4,'PENDING',$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15) RETURNING id`, workspace, currentUserID(c), in.TargetUserID, kind, in.PeriodStart, in.PeriodEnd, filename, idem, fp, in.Instructions, mappings, in.Description, snapshot, s.config.Tasks.MaxRounds, in.RelatedImportID).Scan(&id)
	if err != nil {
		_ = tx.Rollback()
		if id, e := existing(c.Request.Context()); e == nil {
			return id, false, nil
		} else if errors.Is(e, errIdempotency) {
			return 0, false, e
		}
		return 0, false, err
	}

	if kind == "IMAGE_AI" {
		_, err = tx.ExecContext(c.Request.Context(), `INSERT INTO import_files(import_job_id,storage_key,original_name,media_type,byte_size,sha256) VALUES($1,$2,$3,$4,$5,$6)`, id, key, filename, mime, len(content), digest[:])
		if err != nil {
			return 0, false, err
		}
	}
	if err = importjob.AddOutbox(c.Request.Context(), tx, uint64(id), 1); err != nil {
		return 0, false, err
	}
	if err = writeImportAudit(c.Request.Context(), tx, workspace, currentUserID(c), uint64(id), "AI_IMPORT_CREATED"); err != nil {
		return 0, false, err
	}
	if err = tx.Commit(); err != nil {
		// 提交结果未知时保留输入文件，避免已入库任务失去原图
		check, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if original, e := existing(check); e == nil {
			return original, true, nil
		}
		return uint64(id), true, err
	}
	return uint64(id), true, nil
}
func writeImportAudit(ctx context.Context, tx *sql.Tx, workspace, user, job uint64, action string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(workspace_id,actor_user_id,action,target_type,target_id) VALUES($1,$2,$3,'import_job',$4)`, workspace, user, action, strconv.FormatUint(job, 10))
	return err
}
func (s *server) retryImport(c *gin.Context) {
	workspace, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	if !s.config.AI.Enabled {
		failure(c, 503, "AI_DISABLED", "AI 导入未启用", nil)
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法开始重试", nil)
		return
	}
	defer tx.Rollback()
	var state, kind string
	var target, g uint64
	if tx.QueryRowContext(c.Request.Context(), `SELECT state,import_type,target_user_id,run_generation FROM import_jobs WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, job, workspace).Scan(&state, &kind, &target, &g) != nil {
		failure(c, 404, "IMPORT_NOT_FOUND", "任务不存在", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspace, target) {
		return
	}
	if state != "FAILED" || (kind != "TEXT_AI" && kind != "IMAGE_AI") {
		failure(c, 409, "IMPORT_NOT_RETRYABLE", "仅失败的 AI 任务可以重试", nil)
		return
	}
	if kind == "IMAGE_AI" {
		var key string
		if tx.QueryRowContext(c.Request.Context(), `SELECT storage_key FROM import_files WHERE import_job_id=$1`, job).Scan(&key) != nil {
			failure(c, 409, "INPUT_MISSING", "原图不存在", nil)
			return
		}
		reader, e := s.store.Open(c.Request.Context(), key)
		if e != nil {
			failure(c, 409, "INPUT_MISSING", "原图不存在", nil)
			return
		}
		reader.Close()
	}
	if !s.config.AI.Supports(kind == "IMAGE_AI") {
		failure(c, 503, "NO_AVAILABLE_PROVIDER", "没有配置可用模型", nil)
		return
	}
	_, err = tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET state='PENDING',stage='WAITING',run_generation=run_generation+1,attempt_count=0,max_attempts=$1,lease_owner=NULL,lease_expires_at=NULL,retry_not_before=NULL,error_code=NULL,error_message=NULL WHERE id=$2`, s.config.Tasks.MaxRounds, job)
	if err == nil {
		err = importjob.AddOutbox(c.Request.Context(), tx, job, g+1)
	}
	if err == nil {
		err = writeImportAudit(c.Request.Context(), tx, workspace, currentUserID(c), job, "AI_IMPORT_RETRY")
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法投递重试", nil)
		return
	}
	success(c, 200, gin.H{"id": job, "state": "PENDING", "runGeneration": g + 1})
}
func (s *server) attempts(c *gin.Context) {
	_, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(c.Request.Context(), `SELECT id,generation,round,state,started_at,finished_at FROM import_attempts WHERE job_id=$1 ORDER BY generation,round`, job)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取调用记录", nil)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, g uint64
		var round int
		var state string
		var a time.Time
		var b sql.NullTime
		if rows.Scan(&id, &g, &round, &state, &a, &b) != nil {
			failure(c, 500, "DATABASE_ERROR", "调用记录读取失败", nil)
			return
		}
		items = append(items, gin.H{"id": id, "generation": g, "round": round, "state": state, "startedAt": a, "finishedAt": b.Time})
	}
	if rows.Err() != nil {
		failure(c, 500, "DATABASE_ERROR", "调用记录读取失败", nil)
		return
	}
	success(c, 200, gin.H{"items": items})
}
func (s *server) calls(c *gin.Context) {
	_, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	aid, ok := parseID(c, "attemptId")
	if !ok {
		return
	}
	rows, err := s.db.QueryContext(c.Request.Context(), `SELECT c.id,c.provider_id,c.model,c.sequence,c.started_at,c.finished_at,c.http_status,c.code,c.finish_reason,c.output_tokens FROM import_provider_calls c JOIN import_attempts a ON a.id=c.attempt_id WHERE a.job_id=$1 AND a.id=$2 ORDER BY c.sequence`, job, aid)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取供应商记录", nil)
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id uint64
		var provider, model, code, finish string
		var seq, status, tokens int
		var a time.Time
		var b sql.NullTime
		if rows.Scan(&id, &provider, &model, &seq, &a, &b, &status, &code, &finish, &tokens) != nil {
			failure(c, 500, "DATABASE_ERROR", "读取失败", nil)
			return
		}
		items = append(items, gin.H{"id": id, "providerId": provider, "model": model, "sequence": seq, "startedAt": a, "finishedAt": b.Time, "httpStatus": status, "code": code, "finishReason": finish, "outputTokens": tokens})
	}
	if rows.Err() != nil {
		failure(c, 500, "DATABASE_ERROR", "读取失败", nil)
		return
	}
	success(c, 200, gin.H{"items": items})
}
func (s *server) callRaw(c *gin.Context) {
	_, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	aid, ok := parseID(c, "attemptId")
	if !ok {
		return
	}
	cid, ok := parseID(c, "callId")
	if !ok {
		return
	}
	var raw string
	err := s.db.QueryRowContext(c.Request.Context(), `SELECT COALESCE(c.raw_text,'') FROM import_provider_calls c JOIN import_attempts a ON a.id=c.attempt_id WHERE a.job_id=$1 AND a.id=$2 AND c.id=$3`, job, aid, cid).Scan(&raw)
	if err != nil {
		failure(c, 404, "CALL_NOT_FOUND", "调用记录不存在", nil)
		return
	}
	success(c, 200, gin.H{"raw": raw})
}
func (s *server) requireReviewVersion(c *gin.Context, tx *sql.Tx, job uint64, expected uint64) bool {
	var version uint64
	if tx.QueryRowContext(c.Request.Context(), `SELECT review_version FROM import_jobs WHERE id=$1 FOR UPDATE`, job).Scan(&version) != nil {
		failure(c, 404, "IMPORT_NOT_FOUND", "导入不存在", nil)
		return false
	}
	if expected == 0 || expected != version {
		failure(c, 409, "STALE_REVIEW", "审查已更新，请刷新后重试", gin.H{"reviewVersion": version})
		return false
	}
	return true
}
func (s *server) refreshPreview(c *gin.Context) {
	workspace, job, ok := s.parseImportScope(c)
	if !ok {
		return
	}
	var request struct {
		Expected uint64 `json:"expectedReviewVersion"`
	}
	if c.ShouldBindJSON(&request) != nil {
		failure(c, 400, "INVALID_REQUEST", "请求无效", nil)
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法刷新", nil)
		return
	}
	defer tx.Rollback()
	var state string
	var target uint64
	if tx.QueryRowContext(c.Request.Context(), `SELECT state,target_user_id FROM import_jobs WHERE id=$1 FOR UPDATE`, job).Scan(&state, &target) != nil || state != "NEEDS_REVIEW" {
		failure(c, 409, "IMPORT_NOT_REVIEWABLE", "当前不可刷新", nil)
		return
	}
	if !s.requireImportWriterTx(c, tx, workspace, target) || !s.requireReviewVersion(c, tx, job, request.Expected) {
		return
	}
	items, err := s.loadImportItems(c.Request.Context(), tx, job)
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法读取预览", nil)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	for _, item := range items {
		current, found, e := queryScheduleTx(c.Request.Context(), tx, workspace, target, item.Date, true)
		if e != nil {
			failure(c, 500, "DATABASE_ERROR", "无法读取基线", nil)
			return
		}
		var id, version any
		kind := item.Type
		decision := any(item.Decision)
		changed := (item.ExistingScheduleID == nil && found) || (item.ExistingScheduleID != nil && (!found || current.ID != *item.ExistingScheduleID || item.ExistingVersion == nil || current.Version != *item.ExistingVersion))
		if found {
			id, version = current.ID, current.Version
		}
		if changed {
			decision = nil
		}
		if item.Draft != nil && kind != "UNCERTAIN" && kind != "INVALID" && kind != "MISSING" {
			kind = "NEW"
			if found {
				kind = "CONFLICT"
				if schedulesEquivalent(*item.Draft, storedFromDay(current)) {
					kind = "SAME"
				}
			}
		}
		if _, e = tx.ExecContext(c.Request.Context(), `UPDATE import_items SET item_type=$1,existing_schedule_id=$2,existing_version=$3,decision=$4 WHERE id=$5`, kind, id, version, decision, item.ID); e != nil {
			failure(c, 500, "DATABASE_ERROR", "无法更新预览", nil)
			return
		}
	}
	_, err = tx.ExecContext(c.Request.Context(), `UPDATE import_jobs SET review_version=review_version+1,conflict_count=(SELECT COUNT(*) FROM import_items WHERE import_job_id=$1 AND item_type='CONFLICT') WHERE id=$2`, job, job)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		failure(c, 500, "DATABASE_ERROR", "无法保存刷新", nil)
		return
	}
	success(c, 200, gin.H{"reviewVersion": request.Expected + 1})
}
