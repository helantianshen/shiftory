// Package logging 按配置创建文本或 JSON 格式的 slog 日志器
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New 创建进程日志器，支持 debug、info、warn、error 级别及 text、json 格式
// output 为 nil 时日志写入 stdout
func New(level, format string, output io.Writer) (*slog.Logger, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	format = strings.ToLower(strings.TrimSpace(format))
	if output == nil {
		output = os.Stdout
	}
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		return nil, &InvalidSetting{Field: "log level", Value: level}
	}
	options := &slog.HandlerOptions{Level: slogLevel, AddSource: slogLevel <= slog.LevelDebug}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(output, options)
	case "json":
		handler = slog.NewJSONHandler(output, options)
	default:
		return nil, &InvalidSetting{Field: "log format", Value: format}
	}
	return slog.New(handler), nil
}

// InvalidSetting 描述日志等级或格式的无效配置值
type InvalidSetting struct {
	Field string
	Value string
}

// Error 返回无效日志配置项的错误说明
func (e *InvalidSetting) Error() string {
	return "invalid " + e.Field + " " + e.Value
}
