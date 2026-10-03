package importjob

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"shiftory-server/internal/ai"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/schedule"
)

const taskType = "shiftory:ai_import:v1"

type Message struct {
	Version    int    `json:"version"`
	JobID      uint64 `json:"jobId"`
	Generation uint64 `json:"runGeneration"`
}

func TaskID(job, generation uint64) string { return fmt.Sprintf("shiftory-ai-%d-%d", job, generation) }
func AddOutbox(ctx context.Context, tx *sql.Tx, job, generation uint64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO import_outbox(job_id,generation) VALUES($1,$2)`, job, generation)
	return err
}

type Queue struct {
	db        *sql.DB
	cfg       config.Config
	processor Processor
	server    *asynq.Server
	client    *asynq.Client
	inspector *asynq.Inspector
	Redis     *redis.Client
	logger    *slog.Logger
}

func RedisOptions(c config.RedisConfig) *redis.Options {
	o := &redis.Options{Addr: c.Address(), Username: c.Username, Password: c.Password, DB: c.DB, DialTimeout: c.DialTimeout, ReadTimeout: c.ReadTimeout, WriteTimeout: c.WriteTimeout}
	if c.TLS {
		o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return o
}
func NewQueue(db *sql.DB, cfg config.Config, processor Processor, rc *redis.Client, logger *slog.Logger) *Queue {
	o := asynq.RedisClientOpt{Addr: cfg.Redis.Address(), Username: cfg.Redis.Username, Password: cfg.Redis.Password, DB: cfg.Redis.DB, DialTimeout: cfg.Redis.DialTimeout, ReadTimeout: cfg.Redis.ReadTimeout, WriteTimeout: cfg.Redis.WriteTimeout}
	if cfg.Redis.TLS {
		o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	q := &Queue{db: db, cfg: cfg, processor: processor, Redis: rc, logger: logger, client: asynq.NewClient(o), inspector: asynq.NewInspector(o)}
	q.server = asynq.NewServer(o, asynq.Config{Concurrency: cfg.Tasks.Concurrency, Queues: map[string]int{cfg.Tasks.Queue: 1}, ShutdownTimeout: cfg.Tasks.ShutdownTimeout, RetryDelayFunc: func(n int, e error, t *asynq.Task) time.Duration {
		delay := cfg.Tasks.RetryInitial * time.Duration(1<<min(n, 10))
		if delay > cfg.Tasks.RetryMax {
			delay = cfg.Tasks.RetryMax
		}
		var pe *ai.ProcessingError
		if errors.As(e, &pe) && time.Until(pe.RetryAt) > delay {
			delay = time.Until(pe.RetryAt)
		}
		return delay
	}, ErrorHandler: asynq.ErrorHandlerFunc(q.finalizeQueueFailure), IsFailure: func(e error) bool { var busy *deferredError; return !errors.As(e, &busy) }, Logger: quietQueueLogger{logger}})
	return q
}

// 队列日志只输出基础设施状态，不记录任务正文与底层错误内容
type quietQueueLogger struct{ l *slog.Logger }

func (l quietQueueLogger) Debug(...interface{}) {}
func (l quietQueueLogger) Info(...interface{})  {}
func (l quietQueueLogger) Warn(...interface{})  { l.l.Warn("AI queue warning") }
func (l quietQueueLogger) Error(...interface{}) { l.l.Error("AI queue infrastructure error") }
func (l quietQueueLogger) Fatal(...interface{}) { l.l.Error("AI queue stopped") }

type deferredError struct{ cause error }

func (e *deferredError) Unwrap() error { return e.cause }
func (*deferredError) Error() string   { return "AI execution deferred" }
func (q *Queue) Start() error {
	m := asynq.NewServeMux()
	m.HandleFunc(taskType, q.handle)
	return q.server.Start(m)
}
func (q *Queue) Stop() { q.server.Stop() }
func (q *Queue) Close() {
	q.server.Stop()
	q.server.Shutdown()
	_ = q.client.Close()
	_ = q.inspector.Close()
}
func (q *Queue) Cancel(ctx context.Context, job, generation uint64) {
	_ = q.inspector.CancelProcessing(TaskID(job, generation))
}
func (q *Queue) Ready(ctx context.Context) bool { return q.Redis.Ping(ctx).Err() == nil }

func (q *Queue) Dispatch(ctx context.Context) {
	ticker := time.NewTicker(q.cfg.Tasks.OutboxPollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		for i := 0; i < 50; i++ {
			worked, err := q.dispatchOne(ctx)
			if err != nil {
				q.logger.Warn("AI outbox delivery deferred")
				break
			}
			if !worked {
				break
			}
		}
		q.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (q *Queue) dispatchOne(ctx context.Context) (bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, job, generation uint64
	err = tx.QueryRowContext(ctx, `SELECT id,job_id,generation FROM import_outbox WHERE state='PENDING' AND next_at<=statement_timestamp() AND (lease_until IS NULL OR lease_until<statement_timestamp()) ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &job, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	token := uuid.NewString()
	_, err = tx.ExecContext(ctx, `UPDATE import_outbox SET token=$1,lease_until=$2 WHERE id=$3`, token, time.Now().UTC().Add(30*time.Second), id)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	payload, _ := json.Marshal(Message{1, job, generation})
	_, err = q.client.EnqueueContext(ctx, asynq.NewTask(taskType, payload), asynq.Queue(q.cfg.Tasks.Queue), asynq.TaskID(TaskID(job, generation)), asynq.MaxRetry(q.cfg.Tasks.MaxRounds-1), asynq.Timeout(q.cfg.AI.RoundTimeout+10*time.Second), asynq.Retention(24*time.Hour))
	if err == nil || errors.Is(err, asynq.ErrTaskIDConflict) {
		_, e := q.db.ExecContext(ctx, `UPDATE import_outbox SET state='DELIVERED',token=NULL,lease_until=NULL,error_code=NULL WHERE id=$1 AND token=$2`, id, token)
		return true, e
	}
	_, e := q.db.ExecContext(ctx, `UPDATE import_outbox SET token=NULL,lease_until=NULL,next_at=$1,error_code='REDIS_UNAVAILABLE' WHERE id=$2 AND token=$3`, time.Now().UTC().Add(q.cfg.Tasks.RetryInitial), id, token)
	if e != nil {
		return true, e
	}
	return true, err
}
func (q *Queue) claim(ctx context.Context, m Message) (*Job, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var j Job
	var start, end, state string
	var owner sql.NullString
	var expires sql.NullTime
	var mappings []byte
	err = tx.QueryRowContext(ctx, `SELECT id,workspace_id,upload_user_id,target_user_id,import_type,to_char(period_start, 'YYYY-MM-DD'),to_char(period_end, 'YYYY-MM-DD'),source_filename,COALESCE(description,''),COALESCE(recognition_instructions,''),mapping_hints,input_snapshot,run_generation,state,lease_owner,lease_expires_at,attempt_count,max_attempts FROM import_jobs WHERE id=$1 FOR UPDATE`, m.JobID).Scan(&j.ID, &j.WorkspaceID, &j.UploadUserID, &j.TargetUserID, &j.ImportType, &start, &end, &j.SourceFilename, &j.Description, &j.RecognitionInstructions, &mappings, &j.InputSnapshot, &j.Generation, &state, &owner, &expires, &j.AttemptCount, &j.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if m.Generation != j.Generation || (state != "PENDING" && state != "PARSING") {
		return nil, nil
	}
	if owner.Valid && expires.Valid && expires.Time.After(time.Now().UTC()) {
		return nil, &deferredError{}
	}
	if j.AttemptCount >= j.MaxAttempts {
		_, err = tx.ExecContext(ctx, `UPDATE import_jobs SET state='FAILED',stage='FAILED',error_code='RETRY_BUDGET_EXHAUSTED',error_message='识别重试次数已用完',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1`, j.ID)
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	if j.ImportType == "IMAGE_AI" {
		if err = tx.QueryRowContext(ctx, `SELECT storage_key,media_type FROM import_files WHERE import_job_id=$1`, j.ID).Scan(&j.StorageKey, &j.MediaType); err != nil {
			return nil, err
		}
	}
	j.PeriodStart, err = schedule.ParseDate(start)
	if err != nil {
		return nil, err
	}
	j.PeriodEnd, err = schedule.ParseDate(end)
	if err != nil {
		return nil, err
	}
	j.MappingHints = map[string]string{}
	if len(mappings) > 0 {
		if err = json.Unmarshal(mappings, &j.MappingHints); err != nil {
			return nil, err
		}
	}
	j.LeaseOwner = uuid.NewString()
	j.AttemptCount++
	_, err = tx.ExecContext(ctx, `UPDATE import_attempts SET state='INTERRUPTED',finished_at=statement_timestamp() WHERE job_id=$1 AND generation=$2 AND state='RUNNING'`, j.ID, j.Generation)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE import_jobs SET state='PARSING',stage='RECOGNIZING',attempt_count=$1,lease_owner=$2,lease_expires_at=$3,heartbeat_at=statement_timestamp(),retry_not_before=NULL,error_code=NULL,error_message=NULL WHERE id=$4`, j.AttemptCount, j.LeaseOwner, time.Now().UTC().Add(q.cfg.AI.RoundTimeout+15*time.Second), j.ID)
	if err != nil {
		return nil, err
	}
	var aid int64
	err = tx.QueryRowContext(ctx, `INSERT INTO import_attempts(job_id,generation,round,state) VALUES($1,$2,$3,'RUNNING') RETURNING id`, j.ID, j.Generation, j.AttemptCount).Scan(&aid)
	if err != nil {
		return nil, err
	}

	j.AttemptID = uint64(aid)
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &j, nil
}
func (q *Queue) handle(ctx context.Context, t *asynq.Task) error {
	var m Message
	if json.Unmarshal(t.Payload(), &m) != nil || m.Version != 1 || m.JobID == 0 || m.Generation == 0 {
		return asynq.SkipRetry
	}
	j, err := q.claim(ctx, m)
	if err != nil {
		return err
	}
	if j == nil {
		_ = q.Redis.Incr(ctx, "shiftory:ai:metrics:"+q.cfg.Tasks.Queue+":idempotent").Err()
		return nil
	}
	processCtx, cancel := context.WithTimeout(ctx, q.cfg.AI.RoundTimeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(q.cfg.Tasks.CancellationPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-processCtx.Done():
				return
			case <-ticker.C:
				res, e := q.db.ExecContext(processCtx, `UPDATE import_jobs SET heartbeat_at=statement_timestamp(),lease_expires_at=$1 WHERE id=$2 AND state='PARSING' AND run_generation=$3 AND lease_owner=$4 AND lease_expires_at>statement_timestamp()`, time.Now().UTC().Add(q.cfg.AI.RoundTimeout+15*time.Second), j.ID, j.Generation, j.LeaseOwner)
				if e != nil {
					cancel()
					return
				}
				n, e := res.RowsAffected()
				if e != nil || n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	result, processErr := q.processor.Process(processCtx, *j)
	lost := processCtx.Err() != nil
	cancel()
	<-done
	saveCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	repository := NewPostgresRepository(q.db)
	if processErr == nil && !lost {
		if err = repository.Complete(saveCtx, *j, result); err != nil {
			return errors.New("AI result persistence failed")
		}
		return nil
	}
	if processErr == nil {
		processErr = errors.New("AI execution interrupted")
	}
	var pe *ai.ProcessingError
	retry := lost || IsRetryable(processErr)
	code, hint := "AI_PROCESSING_FAILED", "识别失败，请检查输入或重试"
	if errors.As(processErr, &pe) {
		retry = pe.Retryable
		code, hint = pe.Code, pe.SafeHint
	}
	if pe != nil && (pe.Code == "PROVIDER_DEFERRED" || (pe.Code == "QUEUE_UNAVAILABLE" && pe.Retryable)) {
		tx, e := q.db.BeginTx(saveCtx, nil)
		if e != nil {
			return errors.New("deferred state persistence failed")
		}
		defer tx.Rollback()
		res, e := tx.ExecContext(saveCtx, `UPDATE import_jobs SET state='PENDING',stage='WAITING_PROVIDER',attempt_count=attempt_count-1,lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=NULL,retry_not_before=$1 WHERE id=$2 AND run_generation=$3 AND state='PARSING' AND lease_owner=$4 AND lease_expires_at>statement_timestamp() AND NOT EXISTS(SELECT 1 FROM import_provider_calls WHERE attempt_id=$5)`, pe.RetryAt, j.ID, j.Generation, j.LeaseOwner, j.AttemptID)
		if e != nil {
			return errors.New("deferred state persistence failed")
		}
		n, _ := res.RowsAffected()
		if n == 1 {
			if _, e = tx.ExecContext(saveCtx, `DELETE FROM import_attempts WHERE id=$1`, j.AttemptID); e != nil {
				return errors.New("deferred diagnostic persistence failed")
			}
			if e = tx.Commit(); e != nil {
				return errors.New("deferred state persistence failed")
			}
			if pe.Code == "PROVIDER_DEFERRED" {
				return &deferredError{cause: pe}
			}
			return pe
		}
		if e = tx.Rollback(); e != nil {
			return errors.New("deferred transaction release failed")
		}
	}

	n, _ := asynq.GetRetryCount(ctx)
	max, _ := asynq.GetMaxRetry(ctx)
	state := "FAILED"
	next := any(nil)
	if retry && j.AttemptCount < j.MaxAttempts && n < max {
		state = "PENDING"
		delay := q.cfg.Tasks.RetryInitial * time.Duration(1<<min(n, 10))
		if delay > q.cfg.Tasks.RetryMax {
			delay = q.cfg.Tasks.RetryMax
		}
		next = time.Now().UTC().Add(delay)
	}
	res, err := q.db.ExecContext(saveCtx, `UPDATE import_jobs SET state=$1,stage=$2,error_code=$3,error_message=$4,retry_not_before=$5,lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=NULL WHERE id=$6 AND state='PARSING' AND run_generation=$7 AND lease_owner=$8 AND lease_expires_at>statement_timestamp()`, state, state, code, hint, next, j.ID, j.Generation, j.LeaseOwner)
	if err != nil {
		return errors.New("AI failure persistence failed")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		var current string
		e := q.db.QueryRowContext(saveCtx, `SELECT state FROM import_jobs WHERE id=$1`, j.ID).Scan(&current)
		if e == nil && current != "PARSING" && current != "PENDING" {
			return nil
		}
		return errors.New("AI execution ownership lost")
	}
	_, err = q.db.ExecContext(saveCtx, `UPDATE import_attempts SET state=$1,finished_at=statement_timestamp() WHERE id=$2 AND state='RUNNING'`, state, j.AttemptID)
	if err != nil {
		return errors.New("AI attempt persistence failed")
	}
	if state == "FAILED" {
		return fmt.Errorf("%s: %w", code, asynq.SkipRetry)
	}
	if pe != nil {
		return pe
	}
	return errors.New("AI execution retry required")
}
func (q *Queue) finalizeQueueFailure(ctx context.Context, t *asynq.Task, e error) {
	n, _ := asynq.GetRetryCount(ctx)
	max, _ := asynq.GetMaxRetry(ctx)
	if n < max && !errors.Is(e, asynq.SkipRetry) {
		return
	}
	var m Message
	if json.Unmarshal(t.Payload(), &m) != nil {
		return
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = q.db.ExecContext(c, `UPDATE import_jobs SET state='FAILED',stage='FAILED',error_code='QUEUE_RETRIES_EXHAUSTED',error_message='任务调度重试已结束',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1 AND run_generation=$2 AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp())`, m.JobID, m.Generation)
}
func (q *Queue) reconcile(ctx context.Context) {
	rows, err := q.db.QueryContext(ctx, `SELECT id,run_generation FROM import_jobs WHERE import_type IN ('IMAGE_AI','TEXT_AI') AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp()) AND EXISTS(SELECT 1 FROM import_outbox o WHERE o.job_id=import_jobs.id AND o.generation=import_jobs.run_generation AND o.state='DELIVERED') ORDER BY id LIMIT 50`)
	if err != nil {
		return
	}
	type pair struct{ id, g uint64 }
	jobs := []pair{}
	for rows.Next() {
		var p pair
		if rows.Scan(&p.id, &p.g) != nil {
			break
		}
		jobs = append(jobs, p)
	}
	rows.Close()
	for _, j := range jobs {
		info, e := q.inspector.GetTaskInfo(q.cfg.Tasks.Queue, TaskID(j.id, j.g))
		if errors.Is(e, asynq.ErrTaskNotFound) {
			_, _ = q.db.ExecContext(ctx, `UPDATE import_jobs SET state='FAILED',stage='FAILED',error_code='QUEUE_MESSAGE_MISSING',error_message='队列消息不存在，请人工重新发起',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1 AND run_generation=$2 AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp())`, j.id, j.g)
			continue
		}
		if e != nil {
			continue
		}
		if info.State != asynq.TaskStateArchived {
			continue
		}
		_, _ = q.db.ExecContext(ctx, `UPDATE import_jobs SET state='FAILED',stage='FAILED',error_code='QUEUE_ARCHIVED',error_message='队列任务已归档，请人工重试',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1 AND run_generation=$2 AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp())`, j.id, j.g)
	}
}
