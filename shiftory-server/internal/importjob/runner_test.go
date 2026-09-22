package importjob

import (
	"testing"
	"time"
)

// TestRunnerUsesConfiguredMaximumConcurrency 验证 Runner 协程池容量采用配置值
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

// TestRunnerNotifyIsNonBlockingAndCoalesced 验证连续通知不会阻塞且唤醒信号会合并
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
