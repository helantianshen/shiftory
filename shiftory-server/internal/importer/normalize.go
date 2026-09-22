package importer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"shiftory-server/internal/schedule"
)

var expectedHeaders = []string{"日期", "状态", "班次", "开始时间", "结束时间", "是否跨日", "备注"}

// ShiftMapping 提供工作区班次及其时间、颜色快照用于表格别名映射
type ShiftMapping struct {
	ID           uint64
	Name         string
	Code         string
	StartTime    *string
	EndTime      *string
	CrossDay     bool
	DisplayColor string
}

// Entry 保存规范化后的逐日草稿及需要人工确认的问题
type Entry struct {
	Date      schedule.Date
	Row       int
	Status    schedule.Status
	Note      string
	Segments  []schedule.Segment
	Uncertain bool
	Issues    []string
}

// Normalize 校验固定表头并合并同日行，无法映射的班次保留为不确定项
func Normalize(workbook WorkbookData, mappings map[string]ShiftMapping) ([]Entry, error) {
	// 固定列顺序是解析前提，表头不匹配时不尝试推断任意表格结构
	if len(workbook.Rows) < 2 {
		return nil, ErrInvalidTemplate
	}
	for index, expected := range expectedHeaders {
		if cell(workbook.Rows[0], index) != expected {
			return nil, fmt.Errorf("%w: column %d must be %q", ErrInvalidTemplate, index+1, expected)
		}
	}
	// 别名按去空白与小写统一索引，避免表格大小写影响班次匹配
	normalizedMappings := make(map[string]ShiftMapping, len(mappings))
	for alias, mapping := range mappings {
		normalizedMappings[strings.ToLower(strings.TrimSpace(alias))] = mapping
	}
	// 同一日期可由多行组成多个时间段，但状态和非空备注必须一致
	entries := make(map[schedule.Date]*Entry)
	for index, row := range workbook.Rows[1:] {
		if emptyRow(row) {
			continue
		}
		date, err := parseDateCell(cell(row, 0))
		if err != nil {
			return nil, rowValidationError(index+2, []string{"日期"}, "INVALID_DATE", "日期格式无效", "请使用 yyyy-mm-dd 格式，例如 2026-09-05", err)
		}
		status, err := parseStatus(cell(row, 1))
		if err != nil {
			return nil, rowValidationError(index+2, []string{"状态"}, "INVALID_STATUS", "状态无效", "请选择“工作”或“休息”", err)
		}
		note := cell(row, 6)
		entry := entries[date]
		if entry == nil {
			entry = &Entry{Date: date, Row: index + 2, Status: status, Note: note}
			entries[date] = entry
		} else if entry.Status != status || (entry.Note != "" && note != "" && entry.Note != note) {
			return nil, rowValidationError(index+2, []string{"日期", "状态", "备注"}, "DUPLICATE_DATE_CONFLICT", "同一日期的状态或备注不一致", "请合并为同一条记录，或保持相同的状态和备注", nil)
		}
		shiftLabel, startText, endText := cell(row, 2), cell(row, 3), cell(row, 4)
		crossDay, err := parseBoolean(cell(row, 5))
		if err != nil {
			return nil, rowValidationError(index+2, []string{"是否跨日"}, "INVALID_CROSS_DAY", "是否跨日的值无效", "请选择“是”或“否”", err)
		}
		// 休息与工作详情互斥，工作行则在班次引用和自定义时间之间二选一
		if status == schedule.StatusRest {
			if shiftLabel != "" || startText != "" || endText != "" || crossDay {
				return nil, rowValidationError(index+2, []string{"状态", "班次", "开始时间", "结束时间", "是否跨日"}, "REST_HAS_SEGMENTS", "休息日不能填写班次、开始时间、结束时间或跨日", "请清空班次、开始时间和结束时间，并将是否跨日设为“否”", schedule.ErrRestHasSegments)
			}
			continue
		}
		if shiftLabel != "" && (startText != "" || endText != "") {
			return nil, rowValidationError(index+2, []string{"班次", "开始时间", "结束时间"}, "SHIFT_TIME_CONFLICT", "班次与自定义开始/结束时间不能同时填写", "请保留班次，或清空班次后同时填写开始和结束时间", nil)
		}
		if shiftLabel != "" {
			mapping, ok := normalizedMappings[strings.ToLower(shiftLabel)]
			if !ok {
				entry.Uncertain = true
				entry.Issues = append(entry.Issues, fmt.Sprintf("第 %d 行班次 %q 无法映射，请人工选择班次", index+2, shiftLabel))
				continue
			}
			segment := schedule.Segment{Type: schedule.SegmentShift, ShiftID: &mapping.ID, ShiftName: mapping.Name, ShiftCode: mapping.Code, CrossDay: mapping.CrossDay, DisplayColor: mapping.DisplayColor, SortOrder: len(entry.Segments), OriginalLabel: shiftLabel}
			if mapping.StartTime != nil && mapping.EndTime != nil {
				start, startErr := schedule.ParseClock(*mapping.StartTime)
				end, endErr := schedule.ParseClock(*mapping.EndTime)
				if startErr != nil || endErr != nil {
					return nil, rowValidationError(index+2, []string{"班次"}, "INVALID_MAPPED_SHIFT_TIME", "已映射班次的时间配置无效", "请联系管理员修正班次配置", fmt.Errorf("mapped shift has invalid time"))
				}
				segment.StartTime, segment.EndTime = &start, &end
			}
			entry.Segments = append(entry.Segments, segment)
			continue
		}
		if startText != "" || endText != "" {
			start, startErr := schedule.ParseClock(startText)
			end, endErr := schedule.ParseClock(endText)
			if startErr != nil || endErr != nil {
				return nil, rowValidationError(index+2, []string{"开始时间", "结束时间"}, "INVALID_TIME_RANGE", "开始时间或结束时间格式无效", "请使用 HH:mm 格式，并同时填写开始和结束时间", fmt.Errorf("invalid time range"))
			}
			entry.Segments = append(entry.Segments, schedule.Segment{Type: schedule.SegmentTimeRange, StartTime: &start, EndTime: &end, CrossDay: crossDay, SortOrder: len(entry.Segments)})
		} else if crossDay {
			return nil, rowValidationError(index+2, []string{"班次", "开始时间", "结束时间", "是否跨日"}, "CROSS_DAY_WITHOUT_SEGMENT", "跨日必须关联班次或完整时间段", "请填写班次，或同时填写开始时间和结束时间", nil)
		}
	}
	// 合并行后再校验全天时间段，检测跨行重叠并按日期稳定输出预览
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		day := schedule.Day{WorkDate: entry.Date, Status: entry.Status, Segments: entry.Segments}
		if err := day.Validate(); err != nil {
			code, message, hint := scheduleValidationDetails(err)
			return nil, rowValidationError(entry.Row, []string{"状态", "班次", "开始时间", "结束时间", "是否跨日"}, code, message, hint, err)
		}
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date.String() < result[j].Date.String() })
	return result, nil
}

// emptyRow 判断一行是否只含空白单元格
func emptyRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

// parseDateCell 接受横线、斜线或点分隔的日期并统一为领域日期
func parseDateCell(value string) (schedule.Date, error) {
	if date, err := schedule.ParseDate(value); err == nil {
		return date, nil
	}
	for _, layout := range []string{"2006/01/02", "2006.01.02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return schedule.MustDate(parsed.Format("2006-01-02")), nil
		}
	}
	return "", schedule.ErrInvalidDate
}

// parseStatus 将中文或英文工作、休息值映射为正式排班状态
func parseStatus(value string) (schedule.Status, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "工作", "WORKING":
		return schedule.StatusWorking, nil
	case "休息", "REST":
		return schedule.StatusRest, nil
	default:
		return "", schedule.ErrInvalidStatus
	}
}

// parseBoolean 解析表格跨日标记，空单元格按不跨日处理
func parseBoolean(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "否", "false", "0", "no":
		return false, nil
	case "是", "true", "1", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("invalid cross-day value %q", value)
	}
}
