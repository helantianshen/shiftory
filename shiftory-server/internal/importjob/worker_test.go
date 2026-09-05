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

func (r *fakeWorkerRepository) Claim(context.Context, string, time.Duration) (*Job, error) {
	return r.job, nil
}
func (r *fakeWorkerRepository) Heartbeat(context.Context, uint64, string, time.Duration) error {
	return nil
}
func (r *fakeWorkerRepository) Complete(context.Context, Job, Result) error {
	r.completed = true
	return nil
}
func (r *fakeWorkerRepository) Retry(context.Context, Job, error, time.Time) error {
	r.retried = true
	return nil
}
func (r *fakeWorkerRepository) Fail(context.Context, Job, error) error {
	r.failed = true
	return nil
}

type fakeProcessor struct {
	result Result
	err    error
}

func (p fakeProcessor) Process(context.Context, Job) (Result, error) { return p.result, p.err }

func TestWorkerCompletesClaimedJob(t *testing.T) {
	repository := &fakeWorkerRepository{job: &Job{ID: 7, AttemptCount: 1, MaxAttempts: 3}}
	worker := NewWorker(repository, fakeProcessor{result: Result{ItemCount: 2}}, "worker-a", time.Minute)
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked || !repository.completed {
		t.Fatalf("expected completion, worked=%v err=%v repository=%+v", worked, err, repository)
	}
}

func TestWorkerRetriesTransientFailureAndFailsPermanentOrExhausted(t *testing.T) {
	transientRepo := &fakeWorkerRepository{job: &Job{ID: 8, AttemptCount: 1, MaxAttempts: 3}}
	worker := NewWorker(transientRepo, fakeProcessor{err: Retryable(errors.New("model unavailable"))}, "worker-a", time.Minute)
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked || !transientRepo.retried {
		t.Fatalf("expected retry, worked=%v err=%v", worked, err)
	}

	permanentRepo := &fakeWorkerRepository{job: &Job{ID: 9, AttemptCount: 1, MaxAttempts: 3}}
	worker = NewWorker(permanentRepo, fakeProcessor{err: errors.New("invalid image")}, "worker-a", time.Minute)
	if _, err := worker.RunOnce(context.Background()); err != nil || !permanentRepo.failed {
		t.Fatalf("expected permanent failure, err=%v", err)
	}

	exhaustedRepo := &fakeWorkerRepository{job: &Job{ID: 10, AttemptCount: 3, MaxAttempts: 3}}
	worker = NewWorker(exhaustedRepo, fakeProcessor{err: Retryable(errors.New("still unavailable"))}, "worker-a", time.Minute)
	if _, err := worker.RunOnce(context.Background()); err != nil || !exhaustedRepo.failed {
		t.Fatalf("expected exhausted job to fail, err=%v", err)
	}
}

func TestWorkerReturnsIdleWhenNothingCanBeClaimed(t *testing.T) {
	worker := NewWorker(&fakeWorkerRepository{}, fakeProcessor{}, "worker-a", time.Minute)
	worked, err := worker.RunOnce(context.Background())
	if err != nil || worked {
		t.Fatalf("expected idle result, worked=%v err=%v", worked, err)
	}
}
