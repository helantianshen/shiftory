package importjob

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeWorkerRepository struct {
	job       *Job
	completed bool
	retried   bool
	failed    bool
}

// Claim 返回预设任务，不模拟数据库锁与租约竞争
func (r *fakeWorkerRepository) Claim(context.Context, string, time.Duration) (*Job, error) {
	return r.job, nil
}

// Heartbeat 模拟成功续租，不修改任务状态
func (r *fakeWorkerRepository) Heartbeat(context.Context, uint64, string, time.Duration) error {
	return nil
}

// Complete 记录 Worker 调用了任务完成操作
func (r *fakeWorkerRepository) Complete(context.Context, Job, Result) error {
	r.completed = true
	return nil
}

// Retry 记录 Worker 请求重新排队
func (r *fakeWorkerRepository) Retry(context.Context, Job, error, time.Time) error {
	r.retried = true
	return nil
}

// Fail 记录 Worker 将任务置为失败
func (r *fakeWorkerRepository) Fail(context.Context, Job, error) error {
	r.failed = true
	return nil
}

type fakeProcessor struct {
	result Result
	err    error
}

// Process 返回预设结果或错误，供 Worker 分支测试使用
func (p fakeProcessor) Process(context.Context, Job) (Result, error) { return p.result, p.err }

// TestWorkerCompletesClaimedJob 验证领取任务后保存完成结果与轮询摘要
func TestWorkerCompletesClaimedJob(t *testing.T) {
	repository := &fakeWorkerRepository{job: &Job{ID: 7, AttemptCount: 1, MaxAttempts: 3}}
	worker := NewWorker(repository, fakeProcessor{result: Result{ItemCount: 2}}, "worker-a", time.Minute)
	outcome, err := worker.RunOnceDetailed(context.Background())
	if err != nil || outcome.State != "completed" || outcome.JobID != 7 || outcome.Attempt != 1 {
		t.Fatalf("unexpected completion outcome: %+v err=%v", outcome, err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked || !repository.completed {
		t.Fatalf("expected completion, worked=%v err=%v repository=%+v", worked, err, repository)
	}
}

// TestWorkerRetriesTransientFailureAndFailsPermanentOrExhausted 验证暂时错误可重试，永久错误和耗尽次数进入失败状态
func TestWorkerRetriesTransientFailureAndFailsPermanentOrExhausted(t *testing.T) {
	transientRepo := &fakeWorkerRepository{job: &Job{ID: 8, AttemptCount: 1, MaxAttempts: 3}}
	worker := NewWorker(transientRepo, fakeProcessor{err: Retryable(errors.New("model unavailable"))}, "worker-a", time.Minute)
	if outcome, err := worker.RunOnceDetailed(context.Background()); err != nil || outcome.State != "retrying" || outcome.JobID != 8 || outcome.Cause == nil {
		t.Fatalf("unexpected retry outcome: %+v err=%v", outcome, err)
	}
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked || !transientRepo.retried {
		t.Fatalf("expected retry, worked=%v err=%v", worked, err)
	}

	permanentRepo := &fakeWorkerRepository{job: &Job{ID: 9, AttemptCount: 1, MaxAttempts: 3}}
	worker = NewWorker(permanentRepo, fakeProcessor{err: errors.New("invalid image")}, "worker-a", time.Minute)
	if outcome, err := worker.RunOnceDetailed(context.Background()); err != nil || outcome.State != "failed" || outcome.JobID != 9 || outcome.Cause == nil {
		t.Fatalf("unexpected failure outcome: %+v err=%v", outcome, err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil || !permanentRepo.failed {
		t.Fatalf("expected permanent failure, err=%v", err)
	}

	exhaustedRepo := &fakeWorkerRepository{job: &Job{ID: 10, AttemptCount: 3, MaxAttempts: 3}}
	worker = NewWorker(exhaustedRepo, fakeProcessor{err: Retryable(errors.New("still unavailable"))}, "worker-a", time.Minute)
	if _, err := worker.RunOnce(context.Background()); err != nil || !exhaustedRepo.failed {
		t.Fatalf("expected exhausted job to fail, err=%v", err)
	}
}

// TestWorkerReturnsIdleWhenNothingCanBeClaimed 验证无任务时返回空闲状态且不标记已工作
func TestWorkerReturnsIdleWhenNothingCanBeClaimed(t *testing.T) {
	worker := NewWorker(&fakeWorkerRepository{}, fakeProcessor{}, "worker-a", time.Minute)
	if outcome, err := worker.RunOnceDetailed(context.Background()); err != nil || outcome.State != "idle" || outcome.Worked {
		t.Fatalf("unexpected idle outcome: %+v err=%v", outcome, err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || worked {
		t.Fatalf("expected idle result, worked=%v err=%v", worked, err)
	}
}
