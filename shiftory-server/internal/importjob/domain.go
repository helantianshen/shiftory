// Package importjob 管理导入状态与 Postgres 业务事实，Asynq Worker 在 API 进程内执行
package importjob

// State 表示导入任务生命周期状态
type State string

const (
	StateUploaded    State = "UPLOADED"
	StatePending     State = "PENDING"
	StateParsing     State = "PARSING"
	StateNeedsReview State = "NEEDS_REVIEW"
	StateCommitting  State = "COMMITTING"
	StateCompleted   State = "COMPLETED"
	StateFailed      State = "FAILED"
	StateCancelled   State = "CANCELLED"
	StateRolledBack  State = "ROLLED_BACK"
)

var transitions = map[State]map[State]struct{}{
	StateUploaded: {
		StatePending:   {},
		StateFailed:    {},
		StateCancelled: {},
	},
	StatePending: {
		StateParsing:   {},
		StateFailed:    {},
		StateCancelled: {},
	},
	StateParsing: {
		StateNeedsReview: {},
		StateFailed:      {},
		StateCancelled:   {},
	},
	StateNeedsReview: {
		StateCommitting: {},
		StateFailed:     {},
		StateCancelled:  {},
	},
	StateCommitting: {
		StateCompleted: {},
		StateFailed:    {},
	},
	StateCompleted: {
		StateRolledBack: {},
	},
}

// CanTransitionTo 判断当前状态能否迁移到目标状态，不执行持久化
func (s State) CanTransitionTo(next State) bool {
	_, ok := transitions[s][next]
	return ok
}

// Terminal 报告失败、取消或撤销等不能继续迁移的终态
func (s State) Terminal() bool {
	// COMPLETED 仍允许进入 ROLLED_BACK，因此不属于最终状态
	return s == StateFailed || s == StateCancelled || s == StateRolledBack
}
