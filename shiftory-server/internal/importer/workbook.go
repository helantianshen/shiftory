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

type Limits struct {
	MaxSheets  int
	MaxRows    int
	MaxColumns int
	MaxBytes   int64
}

func DefaultLimits() Limits {
	return Limits{MaxSheets: 4, MaxRows: 5000, MaxColumns: 32, MaxBytes: 10 << 20}
}

type WorkbookData struct {
	Rows [][]string
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, ErrWorkbookTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read workbook: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrWorkbookTooLarge
	}
	return data, nil
}

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

func cell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
