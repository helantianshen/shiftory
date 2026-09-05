package importjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shiftory-server/internal/schedule"
)

type Job struct {
	ID                      uint64
	WorkspaceID             uint64
	UploadUserID            uint64
	TargetUserID            uint64
	PeriodStart             schedule.Date
	PeriodEnd               schedule.Date
	SourceFilename          string
	StorageKey              string
	MediaType               string
	RecognitionInstructions string
	MappingHints            map[string]string
	LeaseOwner              string
	AttemptCount            int
	MaxAttempts             int
}

type ResultItem struct {
	Date               schedule.Date
	Type               string
	DraftSnapshot      []byte
	ExistingScheduleID *uint64
	ExistingVersion    *uint64
	Issues             []byte
	ErrorMessage       string
	SortOrder          int
}

type Result struct {
	Items         []ResultItem
	ItemCount     int
	ConflictCount int
	InvalidCount  int
	ModelName     string
	PromptVersion string
	SchemaVersion string
	RawResponse   []byte
}

type WorkerRepository interface {
	Claim(context.Context, string, time.Duration) (*Job, error)
	Heartbeat(context.Context, uint64, string, time.Duration) error
	Complete(context.Context, Job, Result) error
	Retry(context.Context, Job, error, time.Time) error
	Fail(context.Context, Job, error) error
}

type Processor interface {
	Process(context.Context, Job) (Result, error)
}

type Worker struct {
	repository WorkerRepository
	processor  Processor
	id         string
	lease      time.Duration
	now        func() time.Time
}

func NewWorker(repository WorkerRepository, processor Processor, id string, lease time.Duration) *Worker {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	return &Worker{repository: repository, processor: processor, id: id, lease: lease, now: time.Now}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.repository == nil || w.processor == nil || w.id == "" {
		return false, errors.New("worker is not configured")
	}
	job, err := w.repository.Claim(ctx, w.id, w.lease)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	processingContext, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		interval := w.lease / 3
		if interval < time.Second {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-processingContext.Done():
				return
			case <-ticker.C:
				_ = w.repository.Heartbeat(processingContext, job.ID, w.id, w.lease)
			}
		}
	}()
	result, processErr := w.processor.Process(processingContext, *job)
	cancel()
	<-heartbeatDone
	if processErr == nil {
		if err := w.repository.Complete(ctx, *job, result); err != nil {
			return true, fmt.Errorf("complete import job %d: %w", job.ID, err)
		}
		return true, nil
	}
	if IsRetryable(processErr) && job.AttemptCount < job.MaxAttempts {
		next := w.now().UTC().Add(retryDelay(job.AttemptCount))
		if err := w.repository.Retry(ctx, *job, processErr, next); err != nil {
			return true, fmt.Errorf("retry import job %d: %w", job.ID, err)
		}
		return true, nil
	}
	if err := w.repository.Fail(ctx, *job, processErr); err != nil {
		return true, fmt.Errorf("fail import job %d: %w", job.ID, err)
	}
	return true, nil
}

func (w *Worker) Run(ctx context.Context, pollPeriod time.Duration) error {
	if pollPeriod <= 0 {
		pollPeriod = 2 * time.Second
	}
	for {
		worked, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(pollPeriod)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type retryableError struct{ error }

func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{error: err}
}

func IsRetryable(err error) bool {
	var target retryableError
	return errors.As(err, &target)
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 10)
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}
