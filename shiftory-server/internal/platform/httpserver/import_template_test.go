package httpserver

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestScheduleImportTemplateKeepsISODateTextFormat(t *testing.T) {
	workbook, err := newScheduleImportTemplate([]string{"早班", "晚班"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	defer workbook.Close()

	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatalf("write template: %v", err)
	}
	reopened, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Fatalf("reopen template: %v", err)
	}
	defer reopened.Close()

	if got, err := reopened.GetCellValue("排班导入", "A2"); err != nil || got != "2026-09-01" {
		t.Fatalf("unexpected date cell: value=%q err=%v", got, err)
	}
	if cellType, err := reopened.GetCellType("排班导入", "A2"); err != nil || cellType != excelize.CellTypeSharedString {
		t.Fatalf("date cell must remain text: type=%v err=%v", cellType, err)
	}

	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("open xlsx archive: %v", err)
	}
	stylesXML := readZIPEntry(t, archive, "xl/styles.xml")
	sheetXML := readZIPEntry(t, archive, "xl/worksheets/sheet1.xml")
	if !strings.Contains(stylesXML, `numFmtId="49"`) {
		t.Fatalf("template styles must include Excel text format (@): %s", stylesXML)
	}
	if !strings.Contains(sheetXML, `r="A2" s="`) {
		t.Fatalf("date cell A2 must reference the text style: %s", sheetXML)
	}
}

func TestScheduleImportTemplateAddsDropdownsAndCombinationValidation(t *testing.T) {
	workbook, err := newScheduleImportTemplate([]string{"早班", "晚班"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	defer workbook.Close()

	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatalf("write template: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("open xlsx archive: %v", err)
	}
	sheetXML := readZIPEntry(t, archive, "xl/worksheets/sheet1.xml")
	workbookXML := readZIPEntry(t, archive, "xl/workbook.xml")

	for _, ref := range []string{"sqref=\"B2:B1000\"", "sqref=\"C2:C1000\"", "sqref=\"F2:F1000\"", "sqref=\"B2:G1000\""} {
		if !strings.Contains(sheetXML, ref) {
			t.Fatalf("template must contain validation range %s: %s", ref, sheetXML)
		}
	}
	if !strings.Contains(sheetXML, `errorStyle="stop"`) || !strings.Contains(sheetXML, "shiftory") {
		t.Fatalf("template validations must stop invalid input and include guidance: %s", sheetXML)
	}
	if !strings.Contains(sheetXML, "$A$1:$A$2") {
		t.Fatalf("shift validation must reference the hidden option list: %s", sheetXML)
	}
	if !strings.Contains(sheetXML, "AND") || !strings.Contains(sheetXML, "休息") || !strings.Contains(sheetXML, "工作") {
		t.Fatalf("template must contain the status/time combination formula: %s", sheetXML)
	}
	if !strings.Contains(workbookXML, "模板选项") {
		t.Fatalf("template must include hidden option sheet: %s", workbookXML)
	}
}

func readZIPEntry(t *testing.T, archive *zip.Reader, name string) string {
	t.Helper()
	for _, entry := range archive.File {
		if entry.Name != name {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(content)
	}
	t.Fatalf("missing xlsx entry %s", name)
	return ""
}
