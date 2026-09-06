package importjob

import (
	"testing"
	"time"
)

func TestRunnerUsesConfiguredMaximumConcurrency(t *testing.T) {
	runner, err := NewRunner(NewWorker(&fakeWorkerRepository{}, fakeProcessor{}, "worker-a", time.Minute), 3, time.Second)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	defer runner.Close()
	if runner.Capacity() != 3 {
		t.Fatalf("unexpected runner capacity %d", runner.Capacity())
	}
}

func TestRunnerNotifyIsNonBlockingAndCoalesced(t *testing.T) {
	runner, err := NewRunner(NewWorker(&fakeWorkerRepository{}, fakeProcessor{}, "worker-a", time.Minute), 1, time.Second)
	if err != nil {
		t.Fatalf("create runner: %v", err)
	}
	defer runner.Close()
	runner.Notify()
	runner.Notify()
	if len(runner.wakeup) != 1 {
		t.Fatalf("expected one coalesced wakeup, got %d", len(runner.wakeup))
	}
}
