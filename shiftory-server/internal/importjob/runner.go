package importjob

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

const defaultRunnerShutdownTimeout = 15 * time.Second

// Runner submits durable database jobs to a fixed-capacity goroutine pool.
// The database remains the source of truth; wakeup only reduces polling latency.
type Runner struct {
	worker     *Worker
	pool       *ants.Pool
	wakeup     chan struct{}
	pollPeriod time.Duration
	closeOnce  sync.Once
}

func NewRunner(worker *Worker, maxConcurrency int, pollPeriod time.Duration) (*Runner, error) {
	if worker == nil {
		return nil, errors.New("worker is required")
	}
	if maxConcurrency <= 0 {
		return nil, errors.New("worker maximum concurrency must be positive")
	}
	if pollPeriod <= 0 {
		pollPeriod = 2 * time.Second
	}
	pool, err := ants.NewPool(maxConcurrency, ants.WithNonblocking(true))
	if err != nil {
		return nil, err
	}
	return &Runner{worker: worker, pool: pool, wakeup: make(chan struct{}, 1), pollPeriod: pollPeriod}, nil
}

func (r *Runner) Capacity() int {
	if r == nil || r.pool == nil {
		return 0
	}
	return r.pool.Cap()
}

func (r *Runner) Notify() {
	if r == nil {
		return
	}
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.worker == nil || r.pool == nil {
		return errors.New("runner is not configured")
	}
	ticker := time.NewTicker(r.pollPeriod)
	defer ticker.Stop()
	for {
		r.submitAvailable(ctx)
		select {
		case <-ctx.Done():
			r.closePool()
			return ctx.Err()
		case <-r.wakeup:
		case <-ticker.C:
		}
	}
}

func (r *Runner) submitAvailable(ctx context.Context) {
	for r.pool.Free() > 0 {
		err := r.pool.Submit(func() {
			worked, runErr := r.worker.RunOnce(ctx)
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				log.Printf("image worker task failed: %v", runErr)
			}
			if worked {
				r.Notify()
			}
		})
		if err != nil {
			return
		}
	}
}

func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.closePool()
}

func (r *Runner) closePool() {
	r.closeOnce.Do(func() {
		if r.pool == nil {
			return
		}
		_ = r.pool.ReleaseTimeout(defaultRunnerShutdownTimeout)
	})
}
