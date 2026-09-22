package importer

import (
	"bytes"
	"fmt"
	"io"

	"github.com/extrame/xls"
)

// ReadXLS 在文件和行列限制内读取旧版 XLS 的首个工作表
func ReadXLS(reader io.Reader, limits Limits) (WorkbookData, error) {
	data, err := readLimited(reader, limits.MaxBytes)
	if err != nil {
		return WorkbookData{}, err
	}
	workbook, err := xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return WorkbookData{}, fmt.Errorf("open xls: %w", err)
	}
	if workbook.NumSheets() == 0 || workbook.NumSheets() > limits.MaxSheets {
		return WorkbookData{}, ErrWorkbookLimits
	}
	sheet := workbook.GetSheet(0)
	if sheet == nil || int(sheet.MaxRow)+1 > limits.MaxRows {
		return WorkbookData{}, ErrWorkbookLimits
	}
	rows := make([][]string, 0, int(sheet.MaxRow)+1)
	for rowIndex := 0; rowIndex <= int(sheet.MaxRow); rowIndex++ {
		row := sheet.Row(rowIndex)
		if row == nil {
			rows = append(rows, nil)
			continue
		}
		lastColumn := row.LastCol()
		if lastColumn > limits.MaxColumns {
			return WorkbookData{}, ErrWorkbookLimits
		}
		values := make([]string, lastColumn)
		for column := 0; column < lastColumn; column++ {
			values[column] = row.Col(column)
		}
		rows = append(rows, values)
	}
	return WorkbookData{Rows: rows}, nil
}
