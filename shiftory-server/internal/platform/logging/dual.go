package logging

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// dualHandler 将同一条记录分别编码为控制台文本与文件 JSON，不使用异步队列
type dualHandler struct {
	console slog.Handler
	file    slog.Handler
}

func (h *dualHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}
func (h *dualHandler) Handle(ctx context.Context, record slog.Record) error {
	var consoleErr, fileErr error
	if h.console.Enabled(ctx, record.Level) {
		consoleErr = h.console.Handle(ctx, record.Clone())
	}
	if h.file.Enabled(ctx, record.Level) {
		fileErr = h.file.Handle(ctx, record.Clone())
	}
	if fileErr != nil {
		diagnostic := slog.NewRecord(time.Now(), slog.LevelError, "日志文件写入失败，原日志已输出到控制台", 0)
		diagnostic.AddAttrs(slog.Any("error", fileErr))
		consoleErr = errors.Join(consoleErr, h.console.Handle(ctx, diagnostic))
	}
	return errors.Join(consoleErr, fileErr)
}
func (h *dualHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dualHandler{console: h.console.WithAttrs(attrs), file: h.file.WithAttrs(attrs)}
}
func (h *dualHandler) WithGroup(name string) slog.Handler {
	return &dualHandler{console: h.console.WithGroup(name), file: h.file.WithGroup(name)}
}
