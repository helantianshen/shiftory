package importjob

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

func (s State) CanTransitionTo(next State) bool {
	_, ok := transitions[s][next]
	return ok
}

func (s State) Terminal() bool {
	return s == StateFailed || s == StateCancelled || s == StateRolledBack
}
