package importer

import (
	"bytes"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// ReadXLSX 限制文件与解压规模，读取 XLSX 首个工作表并检查行列上限
func ReadXLSX(reader io.Reader, limits Limits) (WorkbookData, error) {
	data, err := readLimited(reader, limits.MaxBytes)
	if err != nil {
		return WorkbookData{}, err
	}
	// 解压总量设有上限，单个 XML 超过内存阈值后由解析库转入临时文件处理
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: int64(limits.MaxBytes) * 20, UnzipXMLSizeLimit: int64(limits.MaxBytes) * 10})
	if err != nil {
		return WorkbookData{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer file.Close()
	sheets := file.GetSheetList()
	if len(sheets) == 0 || len(sheets) > limits.MaxSheets {
		return WorkbookData{}, ErrWorkbookLimits
	}
	rows, err := file.GetRows(sheets[0])
	if err != nil {
		return WorkbookData{}, fmt.Errorf("read xlsx rows: %w", err)
	}
	if err := validateLimits(rows, limits); err != nil {
		return WorkbookData{}, err
	}
	return WorkbookData{Rows: rows}, nil
}
