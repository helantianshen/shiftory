// Package logging 创建开发日志器与生产环境的控制台和滚动文件日志器
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"shiftory-server/internal/platform/config"
)

// New 在开发环境保留配置格式，生产环境同步输出控制台文本和滚动 JSON 文件
// output 为 nil 时控制台写入 stdout，返回的文件句柄由调用方关闭
func New(environment string, settings config.LogConfig, output io.Writer) (*slog.Logger, io.Closer, error) {
	level, format := settings.Level, settings.Format
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
		return nil, nil, &InvalidSetting{Field: "log level", Value: level}
	}
	options := &slog.HandlerOptions{Level: slogLevel, AddSource: slogLevel <= slog.LevelDebug}
	if environment == "production" {
		file, err := newRollingWriter(settings.File.Path, int64(settings.File.MaxSizeMB)*1024*1024, settings.File.MaxBackups)
		if err != nil {
			return nil, nil, err
		}
		console := &consoleHandler{output: output, mutex: &sync.Mutex{}, level: slogLevel, source: options.AddSource, color: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
		return slog.New(&dualHandler{console: console, file: slog.NewJSONHandler(file, options)}), file, nil
	}
	var handler slog.Handler
	switch format {
	case "text":
		handler = &consoleHandler{output: output, mutex: &sync.Mutex{}, level: slogLevel, source: options.AddSource, color: os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
	case "json":
		handler = slog.NewJSONHandler(output, options)
	default:
		return nil, nil, &InvalidSetting{Field: "log format", Value: format}
	}
	return slog.New(handler), nil, nil
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
