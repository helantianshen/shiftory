package importjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shiftory-server/internal/schedule"
)

// Job 携带已领取图片任务的业务范围、原文件位置与租约信息
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

// ResultItem 保存逐日预览分类、草稿与既有排班版本锚点
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

// Result 保存图片识别预览及模型版本信息，供仓储整体持久化
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

// WorkerRepository 封装任务领取、租约续期与持有者条件写入
type WorkerRepository interface {
	// Claim 返回本次领取的任务，无任务可领取时返回 nil
	Claim(context.Context, string, time.Duration) (*Job, error)
	// Heartbeat 按任务与领取者标识续期租约
	Heartbeat(context.Context, uint64, string, time.Duration) error
	// Complete 持久化处理结果并进入待审核状态
	Complete(context.Context, Job, Result) error
	// Retry 设置下次允许领取的时间并释放本次租约
	Retry(context.Context, Job, error, time.Time) error
	// Fail 记录处理失败并结束本次租约
	Fail(context.Context, Job, error) error
}

// Processor 将已领取任务转换为审核结果，不负责调度和任务状态持久化
type Processor interface {
	// Process 处理领取任务，返回待持久化结果或可分类的处理错误
	Process(context.Context, Job) (Result, error)
}

// Worker 处理单个领取任务并维护心跳、重试和完成状态
type Worker struct {
	repository WorkerRepository
	processor  Processor
	id         string
	lease      time.Duration
	now        func() time.Time
}

// RunOutcome 描述一次 Worker 轮询结果
// State 取值为 idle、completed、retrying、failed 或 error，处理失败时 Cause 保存原因
type RunOutcome struct {
	Worked  bool
	JobID   uint64
	Attempt int
	State   string
	Cause   error
}

// NewWorker 组合任务仓储与处理器，非正租约使用两分钟默认值
func NewWorker(repository WorkerRepository, processor Processor, id string, lease time.Duration) *Worker {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	return &Worker{repository: repository, processor: processor, id: id, lease: lease, now: time.Now}
}

// RunOnce 处理至多一个任务，返回是否领取过任务及调度错误
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	outcome, err := w.RunOnceDetailed(ctx)
	return outcome.Worked, err
}

// RunOnceDetailed 领取任务并维持心跳，将处理结果落为完成、重试或失败状态
func (w *Worker) RunOnceDetailed(ctx context.Context) (RunOutcome, error) {
	if w == nil || w.repository == nil || w.processor == nil || w.id == "" {
		return RunOutcome{State: "error"}, errors.New("worker is not configured")
	}
	// 每次只领取一个持久化任务，无可领取任务时返回空闲状态
	job, err := w.repository.Claim(ctx, w.id, w.lease)
	if err != nil {
		return RunOutcome{State: "error"}, err
	}
	if job == nil {
		return RunOutcome{State: "idle"}, nil
	}
	outcome := RunOutcome{Worked: true, JobID: job.ID, Attempt: job.AttemptCount, State: "processing"}
	// 识别期间启动续租协程，处理结束后先取消并等待心跳退出
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
				// 完成、重试和失败写入都会再次校验租约所有权，心跳失败不能绕过该校验
				_ = w.repository.Heartbeat(processingContext, job.ID, w.id, w.lease)
			}
		}
	}()
	result, processErr := w.processor.Process(processingContext, *job)
	cancel()
	<-heartbeatDone
	// 处理成功只保存待审核草稿，仓储写入错误作为调度错误返回
	if processErr == nil {
		if err := w.repository.Complete(ctx, *job, result); err != nil {
			outcome.State = "error"
			return outcome, fmt.Errorf("complete import job %d: %w", job.ID, err)
		}
		outcome.State = "completed"
		return outcome, nil
	}
	// 仅标记为暂时失败且未耗尽次数的任务回到等待队列，其余记为失败
	if IsRetryable(processErr) && job.AttemptCount < job.MaxAttempts {
		next := w.now().UTC().Add(retryDelay(job.AttemptCount))
		if err := w.repository.Retry(ctx, *job, processErr, next); err != nil {
			outcome.State = "error"
			return outcome, fmt.Errorf("retry import job %d: %w", job.ID, err)
		}
		outcome.State, outcome.Cause = "retrying", processErr
		return outcome, nil
	}
	if err := w.repository.Fail(ctx, *job, processErr); err != nil {
		outcome.State = "error"
		return outcome, fmt.Errorf("fail import job %d: %w", job.ID, err)
	}
	outcome.State, outcome.Cause = "failed", processErr
	return outcome, nil
}

// Run 持续处理队列，仅在无任务时等待轮询间隔，调度错误立即返回
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

// retryableError 标记处理错误允许进入重试分支
type retryableError struct{ error }

// Retryable 将非空错误标记为允许重试，是否重试还取决于任务剩余次数
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableError{error: err}
}

// IsRetryable 检查错误链是否带有可重试标记
func IsRetryable(err error) bool {
	var target retryableError
	return errors.As(err, &target)
}

// retryDelay 按尝试次数计算有界指数退避时间
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// 重试采用有上限的指数退避，防止外部服务故障期间持续高频请求
	delay := time.Second << min(attempt-1, 10)
	if delay > 30*time.Minute {
		return 30 * time.Minute
	}
	return delay
}
