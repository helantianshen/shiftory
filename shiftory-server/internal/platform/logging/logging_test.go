package logging

import (
	"bytes"
	"strings"
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
	if !strings.Contains(output.String(), `"level":"DEBUG"`) || !strings.Contains(output.String(), `"job_id":7`) {
		t.Fatalf("unexpected JSON output: %q", output.String())
	}
}
