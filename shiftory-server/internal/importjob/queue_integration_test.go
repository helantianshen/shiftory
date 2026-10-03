package importjob

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"io"
	"log/slog"
	"os"
	"shiftory-server/internal/ai"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/schedule"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureProcessor struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	failure bool
}

func (p *fixtureProcessor) Process(ctx context.Context, j Job) (Result, error) {
	p.calls.Add(1)
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	if p.failure {
		return Result{}, Retryable(errors.New("fixture failure"))
	}
	return Result{Items: []ResultItem{{Date: schedule.MustDate("2026-09-01"), Type: "NEW", DraftSnapshot: []byte(`{"status":"REST","note":"","segments":[]}`), Issues: []byte(`[]`)}}, ItemCount: 1, RawResponse: []byte(`{}`)}, nil
}
func TestAsynqOutboxDuplicateCancelAndRetryBudget(t *testing.T) {
	addr := os.Getenv("SHIFTORY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires SHIFTORY_TEST_REDIS_ADDR")
	}
	for _, scenario := range []string{"complete", "cancel", "retry"} {
		t.Run(scenario, func(t *testing.T) {
			db := openWorkerTestDatabase(t)
			seedWorkerJob(t, db, "PENDING", time.Now().UTC())
			if _, err := db.Exec(`UPDATE import_jobs SET attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=1`); err != nil {
				t.Fatal(err)
			}
			cfg, _ := config.Load()
			cfg.Redis.Host = "127.0.0.1"
			cfg.Redis.Port = 16379
			cfg.Redis.DB = 15
			cfg.Tasks.Queue = "shiftory_test_" + scenario
			cfg.Tasks.Concurrency = 2
			cfg.Tasks.RetryInitial = 100 * time.Millisecond
			cfg.Tasks.RetryMax = time.Second
			cfg.Tasks.OutboxPollInterval = 50 * time.Millisecond
			cfg.Tasks.CancellationPollInterval = 20 * time.Millisecond
			cfg.Tasks.ShutdownTimeout = 2 * time.Second
			cfg.AI.RoundTimeout = time.Second
			rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
			defer rc.Close()
			if err := rc.Ping(context.Background()).Err(); err != nil {
				t.Fatal(err)
			}
			p := &fixtureProcessor{failure: scenario == "retry"}
			if scenario == "cancel" {
				p.started = make(chan struct{}, 1)
				p.release = make(chan struct{})
			}
			q := NewQueue(db, cfg, p, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_ = q.inspector.DeleteTask(cfg.Tasks.Queue, TaskID(1, 1))
			defer func() { _ = q.inspector.DeleteTask(cfg.Tasks.Queue, TaskID(1, 1)) }()
			if err := q.Start(); err != nil {
				t.Fatal(err)
			}
			defer q.Close()
			_, err := db.Exec(`INSERT INTO import_outbox(job_id,generation)VALUES(1,1)`)
			if err != nil {
				t.Fatal(err)
			}
			worked, err := q.dispatchOne(context.Background())
			if err != nil || !worked {
				t.Fatal(worked, err)
			}
			// 投递后确认丢失会再次投递同一 TaskID，不能重复生成草稿
			_, _ = db.Exec(`UPDATE import_outbox SET state='PENDING' WHERE job_id=1`)
			if _, err = q.dispatchOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				select {
				case <-p.started:
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not start")
				}
				_, err = db.Exec(`UPDATE import_jobs SET state='CANCELLED',lease_owner=NULL,lease_expires_at=NULL WHERE id=1`)
				if err != nil {
					t.Fatal(err)
				}
				q.Cancel(context.Background(), 1, 1)
				close(p.release)
			}
			desired := map[string]string{"complete": "NEEDS_REVIEW", "cancel": "CANCELLED", "retry": "FAILED"}[scenario]
			deadline := time.Now().Add(20 * time.Second)
			for {
				var state string
				if err := db.QueryRow(`SELECT state FROM import_jobs WHERE id=1`).Scan(&state); err != nil {
					t.Fatal(err)
				}
				if state == desired {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("unexpected queue state", state)
				}
				time.Sleep(50 * time.Millisecond)
			}
			if scenario == "complete" {
				payload, _ := json.Marshal(Message{1, 1, 1})
				if err = q.handle(context.Background(), asynq.NewTask(taskType, payload)); err != nil {
					t.Fatal(err)
				}
				if p.calls.Load() != 1 {
					t.Fatal("duplicate model execution", p.calls.Load())
				}
			}
			if scenario == "retry" && p.calls.Load() != 3 {
				t.Fatal("incorrect retry budget", p.calls.Load())
			}
			var count int
			_ = db.QueryRow(`SELECT COUNT(*) FROM schedule_days`).Scan(&count)
			if count != 0 {
				t.Fatal("worker wrote formal schedule")
			}
		})
	}
}
func TestExecutionGenerationTokenAndExpiryFence(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PENDING", time.Now().UTC())
	cfg, _ := config.Load()
	q := &Queue{db: db, cfg: cfg}
	j, e := q.claim(context.Background(), Message{1, 1, 1})
	if e != nil || j == nil {
		t.Fatal(e)
	}
	if second, e := q.claim(context.Background(), Message{1, 1, 1}); second != nil || e == nil {
		t.Fatal("active lease was stolen")
	}
	_, e = db.Exec(`UPDATE import_jobs SET lease_expires_at=statement_timestamp()-INTERVAL '1 second' WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	r := NewPostgresRepository(db)
	if e = r.Complete(context.Background(), *j, Result{}); e == nil {
		t.Fatal("expired token wrote result")
	}
	newer, e := q.claim(context.Background(), Message{1, 1, 1})
	if e != nil || newer == nil {
		t.Fatal(e)
	}
	if newer.LeaseOwner == j.LeaseOwner {
		t.Fatal("execution token reused")
	}
	if e = r.Complete(context.Background(), *j, Result{}); e == nil {
		t.Fatal("old token wrote result")
	}
	_, _ = db.Exec(`UPDATE import_jobs SET state='PENDING',run_generation=2,lease_owner=NULL,lease_expires_at=NULL WHERE id=1`)
	if old, e := q.claim(context.Background(), Message{1, 1, 1}); old != nil || e != nil {
		t.Fatal("old generation ran")
	}
}

type deferredProcessor struct{}

func (deferredProcessor) Process(context.Context, Job) (Result, error) {
	return Result{}, Retryable(&ai.ProcessingError{Code: "PROVIDER_DEFERRED", Retryable: true, RetryAt: time.Now().Add(time.Minute), SafeHint: "等待供应商"})
}
func TestProviderDeferralDoesNotConsumeRound(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PENDING", time.Now().UTC())
	_, err := db.Exec(`UPDATE import_jobs SET attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	q := &Queue{db: db, cfg: cfg, processor: deferredProcessor{}}
	data, _ := json.Marshal(Message{1, 1, 1})
	err = q.handle(context.Background(), asynq.NewTask(taskType, data))
	var deferred *deferredError
	if !errors.As(err, &deferred) {
		t.Fatal("not deferred", err)
	}
	var state string
	var rounds, attempts int
	if err := db.QueryRow(`SELECT state,attempt_count FROM import_jobs WHERE id=1`).Scan(&state, &rounds); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM import_attempts WHERE job_id=1`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" || rounds != 0 || attempts != 0 {
		t.Fatal("deferral consumed budget", state, rounds, attempts)
	}
}

func TestOutboxRedisOutageAndQueueRestart(t *testing.T) {
	addr := os.Getenv("SHIFTORY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires Redis fixture")
	}
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PENDING", time.Now().UTC())
	_, err := db.Exec(`UPDATE import_jobs SET attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO import_outbox(job_id,generation)VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	cfg.Redis.Host = "127.0.0.1"
	cfg.Redis.Port = 16379
	cfg.Redis.DB = 15
	cfg.Tasks.Queue = "shiftory_test_restart"
	cfg.Tasks.ShutdownTimeout = time.Second
	cfg.Tasks.OutboxPollInterval = 50 * time.Millisecond
	rc := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	defer rc.Close()
	unavailable := cfg
	unavailable.Redis.Port = 1
	unavailable.Redis.DialTimeout = 100 * time.Millisecond
	unavailable.Redis.ReadTimeout = 100 * time.Millisecond
	unavailable.Redis.WriteTimeout = 100 * time.Millisecond
	q := NewQueue(db, unavailable, &fixtureProcessor{}, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err = q.dispatchOne(context.Background()); err == nil {
		t.Fatal("unavailable Redis accepted delivery")
	}
	q.Close()
	var state string
	if err := db.QueryRow(`SELECT state FROM import_outbox WHERE job_id=1`).Scan(&state); err != nil || state != "PENDING" {
		t.Fatal("outbox lost", state, err)
	}
	if _, err = db.Exec(`UPDATE import_outbox SET next_at=statement_timestamp() WHERE job_id=1`); err != nil {
		t.Fatal(err)
	}
	q = NewQueue(db, cfg, &fixtureProcessor{}, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_ = q.inspector.DeleteTask(cfg.Tasks.Queue, TaskID(1, 1))
	if _, err = q.dispatchOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	q.Close()
	p := &fixtureProcessor{}
	q = NewQueue(db, cfg, p, rc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = q.Start(); err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.QueryRow(`SELECT state FROM import_jobs WHERE id=1`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "NEEDS_REVIEW" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue restart lost persisted task", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if p.calls.Load() != 1 {
		t.Fatal("unexpected executions", p.calls.Load())
	}
}

func TestLegacyAdmissionIsExplicitAndIdempotent(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PARSING", time.Now().UTC().Add(-time.Minute))
	count, err := MigrateLegacy(context.Background(), db, 3)
	if err != nil || count != 1 {
		t.Fatal("legacy admission failed", count, err)
	}
	var generation uint64
	var rounds int
	var snapshot []byte
	if err := db.QueryRow(`SELECT run_generation,attempt_count,input_snapshot FROM import_jobs WHERE id=1`).Scan(&generation, &rounds, &snapshot); err != nil {
		t.Fatal(err)
	}
	if generation != 2 || rounds != 0 || len(snapshot) == 0 {
		t.Fatal("legacy input not fixed", generation, rounds)
	}
	count, err = MigrateLegacy(context.Background(), db, 3)
	if err != nil || count != 0 {
		t.Fatal("legacy admission repeated", count, err)
	}
}
