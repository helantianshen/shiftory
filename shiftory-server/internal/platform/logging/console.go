package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type consoleField struct {
	key   string
	value string
}
type consoleHandler struct {
	output        io.Writer
	mutex         *sync.Mutex
	level         slog.Level
	source, color bool
	fields        []consoleField
	groups        []string
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.fields = append([]consoleField(nil), h.fields...)
	for _, attr := range attrs {
		next.fields = appendConsoleAttr(next.fields, h.groups, attr)
	}
	return &next
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.groups = append(append([]string(nil), h.groups...), name)
	return &next
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	fields := append([]consoleField(nil), h.fields...)
	record.Attrs(func(attr slog.Attr) bool { fields = appendConsoleAttr(fields, h.groups, attr); return true })
	if h.source && record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		fields = append(fields, consoleField{"source", fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)})
	}
	sort.SliceStable(fields, func(i, j int) bool { return consoleOrder(fields[i].key) < consoleOrder(fields[j].key) })
	var line strings.Builder
	timestamp := record.Time.Format("2006-01-02 15:04:05.000")
	if h.color {
		line.WriteString("\x1b[90m")
	}
	line.WriteString(timestamp)
	if h.color {
		line.WriteString("\x1b[0m")
	}
	line.WriteString("  ")
	if h.color {
		switch {
		case record.Level >= slog.LevelError:
			line.WriteString("\x1b[31m")
		case record.Level >= slog.LevelWarn:
			line.WriteString("\x1b[33m")
		case record.Level >= slog.LevelInfo:
			line.WriteString("\x1b[32m")
		default:
			line.WriteString("\x1b[36m")
		}
	}
	fmt.Fprintf(&line, "%-5s", record.Level.String())
	if h.color {
		line.WriteString("\x1b[0m")
	}
	line.WriteString("  ")
	line.WriteString(consoleText(record.Message))
	for _, field := range fields {
		line.WriteString(" | ")
		if h.color {
			line.WriteString("\x1b[90m")
		}
		line.WriteString(consoleLabel(field.key))
		line.WriteString(": ")
		if h.color {
			line.WriteString("\x1b[0m")
		}
		line.WriteString(field.value)
	}
	line.WriteByte('\n')
	// 克隆的 Handler 共用写锁，保证并发日志按完整行输出
	h.mutex.Lock()
	defer h.mutex.Unlock()
	_, err := io.WriteString(h.output, line.String())
	return err
}

func appendConsoleAttr(fields []consoleField, groups []string, attr slog.Attr) []consoleField {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return fields
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(append([]string(nil), groups...), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			fields = appendConsoleAttr(fields, groups, child)
		}
		return fields
	}
	key := strings.Join(append(append([]string(nil), groups...), attr.Key), ".")
	value := consoleText(attr.Value.String())
	if key == "duration_ms" {
		value += " ms"
	}
	return append(fields, consoleField{key, value})
}

func consoleText(value string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`, "\t", `\t`, "\x1b", `\x1b`).Replace(value)
}

func consoleOrder(key string) int {
	switch key {
	case "method":
		return 0
	case "path":
		return 1
	case "status":
		return 2
	case "duration_ms":
		return 3
	case "provider_id":
		return 4
	case "model":
		return 5
	case "job_id":
		return 6
	case "attempt_id", "attempt":
		return 7
	case "generation":
		return 8
	case "code", "error_code":
		return 9
	case "http_status":
		return 10
	case "request_id":
		return 90
	case "source":
		return 100
	default:
		return 50
	}
}

func consoleLabel(key string) string {
	switch key {
	case "method":
		return "方法"
	case "path":
		return "路径"
	case "status", "http_status":
		return "HTTP状态"
	case "duration_ms":
		return "耗时"
	case "provider_id":
		return "供应商"
	case "model":
		return "模型"
	case "job_id":
		return "任务"
	case "attempt_id":
		return "执行记录"
	case "attempt":
		return "轮次"
	case "generation":
		return "代次"
	case "code", "error_code":
		return "结果码"
	case "request_id":
		return "请求ID"
	case "source":
		return "来源"
	case "environment":
		return "环境"
	case "log_level":
		return "日志级别"
	case "log_format":
		return "日志格式"
	case "http_addr":
		return "监听地址"
	case "ai_enabled":
		return "AI启用"
	case "workspace_id":
		return "工作区"
	case "target_user_id":
		return "目标成员"
	case "entry_count":
		return "排班条数"
	case "mapping_count":
		return "班次映射数"
	case "period_start":
		return "开始日期"
	case "period_end":
		return "结束日期"
	case "bytes":
		return "字节数"
	case "width":
		return "宽度"
	case "height":
		return "高度"
	case "format":
		return "格式"
	case "error":
		return "错误"
	default:
		return consoleText(key)
	}
}
