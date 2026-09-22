package storage

import (
	"context"
	"io"
)

// Store 以不透明存储键访问原文件，读取器的关闭责任归调用方
type Store interface {
	// Put 将读取器内容保存到指定存储键
	Put(context.Context, string, io.Reader) error
	// Open 打开存储对象，成功后调用方负责关闭读取器
	Open(context.Context, string) (io.ReadCloser, error)
	// Delete 删除指定存储对象
	Delete(context.Context, string) error
}
