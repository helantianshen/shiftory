package importer

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/xuri/excelize/v2"

	"shiftory-server/internal/schedule"
)

func TestReadXLSAndNormalizeFixedTemplate(t *testing.T) {
	file, err := os.Open("testdata/schedule.xls")
	if err != nil {
		t.Fatalf("open xls fixture: %v", err)
	}
	defer file.Close()
	rows, err := ReadXLS(file, DefaultLimits())
	if err != nil {
		t.Fatalf("read xls: %v", err)
	}
	entries, err := Normalize(rows, map[string]ShiftMapping{"早": {ID: 1, Name: "早班", Code: "MORNING"}})
	if err != nil {
		t.Fatalf("normalize xls: %v", err)
	}
	if len(entries) != 2 || entries[0].Note != "旧版表" || entries[1].Status != schedule.StatusRest {
		t.Fatalf("unexpected xls entries: %+v", entries)
	}
}

func TestReadXLSXAndNormalizeFixedTemplate(t *testing.T) {
	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	rows := [][]any{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-04", "工作", "早", "", "", "否", "正常班"},
		{"2026-09-05", "休息", "", "", "", "否", "轮休"},
		{"2026-09-06", "工作", "", "08:30", "12:00", "否", "拆分班"},
		{"2026-09-06", "工作", "", "13:30", "17:30", "否", "拆分班"},
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err := file.SetCellValue(sheet, cell, value); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	workbookRows, err := ReadXLSX(bytes.NewReader(buffer.Bytes()), DefaultLimits())
	if err != nil {
		t.Fatalf("read xlsx: %v", err)
	}
	entries, err := Normalize(workbookRows, map[string]ShiftMapping{
		"早": {ID: 7, Name: "早班", Code: "MORNING", StartTime: stringPtr("08:00"), EndTime: stringPtr("16:00"), DisplayColor: "#22a06b"},
	})
	if err != nil {
		t.Fatalf("normalize rows: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected three days, got %d", len(entries))
	}
	if entries[0].Segments[0].ShiftID == nil || *entries[0].Segments[0].ShiftID != 7 {
		t.Fatalf("shift alias was not mapped: %+v", entries[0])
	}
	if entries[1].Status != schedule.StatusRest || len(entries[1].Segments) != 0 {
		t.Fatalf("rest row normalized incorrectly: %+v", entries[1])
	}
	if len(entries[2].Segments) != 2 {
		t.Fatalf("duplicate date rows were not merged: %+v", entries[2])
	}
}

func TestNormalizeRejectsRestWithWorkDetails(t *testing.T) {
	rows := WorkbookData{Rows: [][]string{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-04", "休息", "早班", "", "", "否", ""},
	}}
	if _, err := Normalize(rows, map[string]ShiftMapping{"早班": {ID: 1, Name: "早班", Code: "MORNING"}}); err == nil {
		t.Fatal("expected rest row with shift to fail")
	}
}

func TestNormalizeReturnsStructuredRestDayError(t *testing.T) {
	rows := WorkbookData{Rows: [][]string{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-05", "休息", "早班", "", "", "否", ""},
	}}
	_, err := Normalize(rows, map[string]ShiftMapping{"早班": {ID: 1, Name: "早班", Code: "MORNING"}})
	if err == nil {
		t.Fatal("expected rest row with shift to fail")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected structured validation error, got %T: %v", err, err)
	}
	if validationErr.Row != 2 || validationErr.Code != "REST_HAS_SEGMENTS" || validationErr.Message != "休息日不能填写班次、开始时间、结束时间或跨日" {
		t.Fatalf("unexpected validation error: %+v", validationErr)
	}
	if !errors.Is(err, schedule.ErrRestHasSegments) {
		t.Fatalf("structured error must preserve domain cause: %v", err)
	}
}

func TestNormalizeReturnsStructuredScheduleError(t *testing.T) {
	rows := WorkbookData{Rows: [][]string{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-05", "工作", "", "08:00", "10:00", "否", ""},
		{"2026-09-05", "工作", "", "09:00", "11:00", "否", ""},
	}}
	_, err := Normalize(rows, nil)
	if err == nil {
		t.Fatal("expected overlapping schedule to fail")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected structured validation error, got %T: %v", err, err)
	}
	if validationErr.Row != 2 || validationErr.Code != "INVALID_SCHEDULE" || validationErr.Message != "该日期的排班组合无效" {
		t.Fatalf("unexpected validation error: %+v", validationErr)
	}
}

func TestNormalizeMarksUnknownShiftUncertainAndRejectsImplicitCrossDay(t *testing.T) {
	unknown := WorkbookData{Rows: [][]string{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-04", "工作", "神秘班", "", "", "否", ""},
	}}
	entries, err := Normalize(unknown, nil)
	if err != nil || len(entries) != 1 || !entries[0].Uncertain || len(entries[0].Issues) != 1 {
		t.Fatalf("expected unknown shift to remain reviewable, got entries=%+v err=%v", entries, err)
	}
	implicit := WorkbookData{Rows: [][]string{
		{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"},
		{"2026-09-04", "工作", "", "20:00", "08:00", "否", ""},
	}}
	if _, err := Normalize(implicit, nil); err == nil {
		t.Fatal("expected implicit cross-day time to fail")
	}
}

func TestReadXLSXEnforcesLimitsAndHeaders(t *testing.T) {
	file := excelize.NewFile()
	if err := file.SetCellValue(file.GetSheetName(0), "A1", "错误表头"); err != nil {
		t.Fatalf("set cell: %v", err)
	}
	buffer, _ := file.WriteToBuffer()
	data, err := ReadXLSX(bytes.NewReader(buffer.Bytes()), Limits{MaxSheets: 1, MaxRows: 10, MaxColumns: 7, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("read workbook: %v", err)
	}
	if _, err := Normalize(data, nil); err == nil {
		t.Fatal("expected invalid header to fail")
	}

	if _, err := ReadXLSX(bytes.NewReader(buffer.Bytes()), Limits{MaxSheets: 1, MaxRows: 10, MaxColumns: 7, MaxBytes: 8}); err == nil {
		t.Fatal("expected byte limit to fail")
	}
}

func stringPtr(value string) *string { return &value }
