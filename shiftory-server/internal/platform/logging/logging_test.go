package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// TestNewFiltersDebugBelowInfo 验证 info 等级不会输出 debug 日志
func TestNewFiltersDebugBelowInfo(t *testing.T) {
	var output bytes.Buffer
	logger, err := New("info", "text", &output)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Debug("hidden debug")
	logger.Info("visible info")
	if strings.Contains(output.String(), "hidden debug") || !strings.Contains(output.String(), "visible info") {
		t.Fatalf("unexpected filtered output: %q", output.String())
	}
}

// TestNewSupportsJSONDebugLogs 验证 JSON 格式能输出 debug 等级的结构化日志
func TestNewSupportsJSONDebugLogs(t *testing.T) {
	var output bytes.Buffer
	logger, err := New("debug", "json", &output)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	logger.Debug("upload started", "job_id", uint64(7))
	if !strings.Contains(output.String(), `"level":"DEBUG"`) || !strings.Contains(output.String(), `"job_id":7`) || strings.Contains(output.String(), "\x1b") {
		t.Fatalf("unexpected JSON output: %q", output.String())
	}
}

func TestConsoleOrdersRoutingFieldsAndEscapesLines(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	logger, err := New("info", "text", &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("AI provider selected", "job_id", 42, "model", "vision-model", "provider_id", "doubao", "detail", "first\nsecond")
	want := "INFO   AI provider selected | 供应商: doubao | 模型: vision-model | 任务: 42 | detail: first\\nsecond\n"
	if !strings.Contains(output.String(), want) || strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "provider_id=") {
		t.Fatalf("unexpected console output: %q", output.String())
	}
}

func TestConsoleGroupsAndConcurrentClones(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	logger, err := New("info", "text", &output)
	if err != nil {
		t.Fatal(err)
	}
	scoped := logger.With("job_id", 7).WithGroup("worker").With("id", "primary")
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			scoped.Info("completed", slog.Group("result", "code", "SUCCESS"))
		}()
	}
	workers.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 20 {
		t.Fatalf("expected 20 complete lines, got %d", len(lines))
	}
	for _, line := range lines {
		if !strings.Contains(line, "任务: 7 | worker.id: primary | worker.result.code: SUCCESS") {
			t.Fatalf("lost attribute scope: %q", line)
		}
	}
	logger.Info("ungrouped")
	if strings.Contains(strings.Split(strings.TrimSpace(output.String()), "\n")[20], "worker") {
		t.Fatal("clone modified parent")
	}
}

func TestConsoleLevelColorsAndDisable(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	var output bytes.Buffer
	logger, err := New("debug", "text", &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")
	for _, color := range []string{"\x1b[36mDEBUG", "\x1b[32mINFO ", "\x1b[33mWARN ", "\x1b[31mERROR"} {
		if !strings.Contains(output.String(), color) {
			t.Fatalf("missing level color: %q", color)
		}
	}
	t.Setenv("TERM", "dumb")
	output.Reset()
	logger, err = New("info", "text", &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("plain")
	if strings.Contains(output.String(), "\x1b") {
		t.Fatal("dumb terminal received colors")
	}
}
