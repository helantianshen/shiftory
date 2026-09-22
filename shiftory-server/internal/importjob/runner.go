package importjob

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

const defaultRunnerShutdownTimeout = 15 * time.Second

// Runner 将数据库中的持久化任务提交到固定容量的 goroutine 池
// 数据库是任务状态的唯一事实来源，wakeup 仅用于缩短轮询延迟
type Runner struct {
	worker     *Worker
	pool       *ants.Pool
	wakeup     chan struct{}
	pollPeriod time.Duration
	logger     *slog.Logger
	closeOnce  sync.Once
}

// NewRunner 使用默认日志器创建有界并发的任务运行器
func NewRunner(worker *Worker, maxConcurrency int, pollPeriod time.Duration) (*Runner, error) {
	return NewRunnerWithLogger(worker, maxConcurrency, pollPeriod, slog.Default())
}

// NewRunnerWithLogger 校验并发配置并创建非阻塞协程池与合并唤醒通道
func NewRunnerWithLogger(worker *Worker, maxConcurrency int, pollPeriod time.Duration, logger *slog.Logger) (*Runner, error) {
	if worker == nil {
		return nil, errors.New("worker is required")
	}
	if maxConcurrency <= 0 {
		return nil, errors.New("worker maximum concurrency must be positive")
	}
	if pollPeriod <= 0 {
		pollPeriod = 2 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	pool, err := ants.NewPool(maxConcurrency, ants.WithNonblocking(true))
	if err != nil {
		return nil, err
	}
	return &Runner{worker: worker, pool: pool, wakeup: make(chan struct{}, 1), pollPeriod: pollPeriod, logger: logger}, nil
}

// Capacity 返回池容量，未初始化的运行器返回零
func (r *Runner) Capacity() int {
	if r == nil || r.pool == nil {
		return 0
	}
	return r.pool.Cap()
}

// Notify 非阻塞发送唤醒信号，已有未消费信号时合并本次通知
func (r *Runner) Notify() {
	if r == nil {
		return
	}
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

// Run 按启动、通知与轮询事件填充空闲槽位，上下文结束时释放池
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

// submitAvailable 为当前空闲槽位提交领取操作，处理过任务后通知继续扫描
func (r *Runner) submitAvailable(ctx context.Context) {
	// 按当前空闲容量提交领取操作，池满时立即退出而不阻塞通知循环
	for r.pool.Free() > 0 {
		err := r.pool.Submit(func() {
			r.logger.Debug("image worker poll started")
			outcome, runErr := r.worker.RunOnceDetailed(ctx)
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				r.logger.Error("image worker poll failed", "error", runErr)
			} else {
				switch outcome.State {
				case "idle":
					r.logger.Debug("image worker poll found no pending jobs")
				case "completed":
					r.logger.Info("image job completed", "job_id", outcome.JobID, "attempt", outcome.Attempt)
				case "retrying":
					r.logger.Warn("image job retry scheduled", "job_id", outcome.JobID, "attempt", outcome.Attempt, "error", outcome.Cause)
				case "failed":
					r.logger.Error("image job failed", "job_id", outcome.JobID, "attempt", outcome.Attempt, "error", outcome.Cause)
				}
			}
			// 处理过任务说明队列可能仍有积压，补发唤醒以继续消耗任务
			if outcome.Worked {
				r.Notify()
			}
		})
		if err != nil {
			return
		}
	}
}

// Close 幂等释放协程池，不负责取消外部传入的任务上下文
func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.closePool()
}

// closePool 只执行一次池释放，最多等待默认停机超时
func (r *Runner) closePool() {
	r.closeOnce.Do(func() {
		if r.pool == nil {
			return
		}
		_ = r.pool.ReleaseTimeout(defaultRunnerShutdownTimeout)
	})
}
