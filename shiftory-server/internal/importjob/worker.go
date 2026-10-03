package importjob

import (
	"context"
	"errors"
	"time"

	"shiftory-server/internal/schedule"
)

// Job 携带已领取图片任务的业务范围、原文件位置与租约信息
type Job struct {
	Generation              uint64
	ImportType              string
	Description             string
	InputSnapshot           []byte
	AttemptID               uint64
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
	RulesSnapshot []byte
	JobIssues     []byte
	Items         []ResultItem
	ItemCount     int
	ConflictCount int
	InvalidCount  int
	ModelName     string
	PromptVersion string
	SchemaVersion string
	RawResponse   []byte
}

// Processor 将已领取任务转换为审核结果，不负责调度和任务状态持久化
type Processor interface {
	// Process 处理领取任务，返回待持久化结果或可分类的处理错误
	Process(context.Context, Job) (Result, error)
}

// retryableError 标记处理错误允许进入重试分支
type retryableError struct{ error }

func (e retryableError) Unwrap() error { return e.error }

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
