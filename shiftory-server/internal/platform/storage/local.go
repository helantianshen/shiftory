// Package storage 通过存储键访问上传原文件，本地实现负责路径检查与文件写入
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local 将存储键映射到配置的本地根目录
type Local struct{ root string }

// NewLocal 将存储根目录转换为绝对路径并创建目录
func NewLocal(root string) (*Local, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, errors.New("local storage root is invalid")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	return &Local{root: absolute}, nil
}

// Put 将数据写入临时文件后替换目标文件，仅在开始写入前检查上下文
func (s *Local) Put(ctx context.Context, key string, reader io.Reader) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// 同目录临时文件确保写入完成前目标路径不可见，并允许原子重命名
	temporary, err := os.CreateTemp(filepath.Dir(path), ".shiftory-upload-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, reader); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

// Open 校验上下文和存储键后打开文件，调用方负责关闭返回的读取器
func (s *Local) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Delete 删除存储键对应的文件，文件不存在时仍视为成功
func (s *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// resolve 将相对存储键解析到根目录内，检查词法路径穿越但不解析符号链接
func (s *Local) resolve(key string) (string, error) {
	key = filepath.Clean(strings.TrimSpace(key))
	if key == "." || filepath.IsAbs(key) {
		return "", errors.New("storage key is invalid")
	}
	// Rel 校验同时覆盖父目录穿越和清理后仍逃逸存储根目录的路径
	path := filepath.Join(s.root, key)
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("storage key escapes root")
	}
	return path, nil
}
