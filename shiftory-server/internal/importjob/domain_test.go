package importjob

import "testing"

// TestJobStateAllowsSuccessfulLifecycle 验证导入任务从上传到完成和撤销的合法迁移
func TestJobStateAllowsSuccessfulLifecycle(t *testing.T) {
	states := []State{
		StateUploaded,
		StatePending,
		StateParsing,
		StateNeedsReview,
		StateCommitting,
		StateCompleted,
		StateRolledBack,
	}
	for i := 0; i < len(states)-1; i++ {
		if !states[i].CanTransitionTo(states[i+1]) {
			t.Fatalf("expected %s -> %s to be allowed", states[i], states[i+1])
		}
	}
}

// TestJobStateAllowsFailureAndCancellationPaths 验证处理中任务允许失败或取消的迁移路径
func TestJobStateAllowsFailureAndCancellationPaths(t *testing.T) {
	if !StateParsing.CanTransitionTo(StateFailed) {
		t.Fatal("parsing must be able to fail")
	}
	if !StatePending.CanTransitionTo(StateCancelled) {
		t.Fatal("pending must be cancellable")
	}
	if !StateNeedsReview.CanTransitionTo(StateCancelled) {
		t.Fatal("review must be cancellable")
	}
}

// TestTerminalJobStatesRejectFurtherTransitions 验证失败、取消与撤销终态不能继续迁移
func TestTerminalJobStatesRejectFurtherTransitions(t *testing.T) {
	for _, state := range []State{StateFailed, StateCancelled, StateRolledBack} {
		if state.CanTransitionTo(StatePending) {
			t.Fatalf("terminal state %s accepted a transition", state)
		}
	}
	if StateCompleted.CanTransitionTo(StatePending) {
		t.Fatal("completed job may only transition to rolled back")
	}
}
