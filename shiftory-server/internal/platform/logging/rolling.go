package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// rollingWriter 只管理当前文件与编号备份，写入和清理共用锁，单个文件只允许一个进程写入
type rollingWriter struct {
	mutex   sync.Mutex
	path    string
	maxSize int64
	backups int
	file    *os.File
	size    int64
	failure error
	closed  bool
}

func newRollingWriter(path string, maxSize int64, backups int) (*rollingWriter, error) {
	if strings.TrimSpace(path) == "" || maxSize <= 0 || backups < 0 {
		return nil, errors.New("invalid log file settings")
	}
	w := &rollingWriter{path: path, maxSize: maxSize, backups: backups}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(path) + "."
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		index, err := strconv.Atoi(suffix)
		if err != nil || index < 1 || strconv.Itoa(index) != suffix {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("log backup is not a regular file")
		}
		if index > backups || info.Size() > maxSize {
			if err := os.Remove(filepath.Join(filepath.Dir(path), entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("log path is not a regular file")
		}
		if info.Size() > maxSize {
			if err := os.Truncate(path, 0); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rollingWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}

// Write 在写入前完成轮转，超大单条记录拒写，文件失败后不继续增加占用
func (w *rollingWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.failure != nil {
		return 0, w.failure
	}
	if int64(len(data)) > w.maxSize {
		return 0, fmt.Errorf("log record exceeds file limit of %d bytes", w.maxSize)
	}
	if w.size > w.maxSize-int64(len(data)) {
		if err := w.rotate(); err != nil {
			w.failure = err
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	if err != nil || n != len(data) {
		if err == nil {
			err = io.ErrShortWrite
		}
		w.failure = errors.Join(err, w.file.Truncate(w.size))
		return 0, w.failure
	}
	w.size += int64(n)
	return n, nil
}

func (w *rollingWriter) rotate() error {
	if w.backups == 0 {
		if err := w.file.Truncate(0); err != nil {
			return err
		}
		w.size = 0
		return nil
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	// 先删除最旧备份，再从旧到新移动编号，整个过程不会多出一份备份
	if err := os.Remove(w.path + "." + strconv.Itoa(w.backups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := w.backups - 1; index >= 1; index-- {
		if err := os.Rename(w.path+"."+strconv.Itoa(index), w.path+"."+strconv.Itoa(index+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return err
	}
	return w.open()
}

func (w *rollingWriter) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
