// Package importer 将固定模板的 XLSX 与 XLS 数据转换为可审核的逐日排班草稿
package importer

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrWorkbookTooLarge = errors.New("workbook exceeds configured byte limit")
	ErrWorkbookLimits   = errors.New("workbook exceeds configured sheet, row, or column limits")
	ErrInvalidTemplate  = errors.New("workbook does not match the Shiftory template")
)

// Limits 约束工作簿大小与行列数量，字节限制包含压缩后的上传内容
type Limits struct {
	MaxSheets  int
	MaxRows    int
	MaxColumns int
	MaxBytes   int64
}

// DefaultLimits 返回工作簿字节数、工作表数及行列数的默认上限
func DefaultLimits() Limits {
	return Limits{MaxSheets: 4, MaxRows: 5000, MaxColumns: 32, MaxBytes: 10 << 20}
}

// WorkbookData 保存首个工作表的文本单元格，首行为模板表头
type WorkbookData struct {
	Rows [][]string
}

// readLimited 最多读取上限加一个字节，用于区分恰好达到上限和超限
func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, ErrWorkbookTooLarge
	}
	// 多读取一个字节才能区分恰好达到上限与超过上限
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read workbook: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrWorkbookTooLarge
	}
	return data, nil
}

// validateLimits 检查已读取二维单元格的行数和逐行列数
func validateLimits(rows [][]string, limits Limits) error {
	if len(rows) > limits.MaxRows {
		return ErrWorkbookLimits
	}
	for _, row := range rows {
		if len(row) > limits.MaxColumns {
			return ErrWorkbookLimits
		}
	}
	return nil
}

// cell 读取并去除单元格首尾空白，缺少该列时返回空字符串
func cell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
