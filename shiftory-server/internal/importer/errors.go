package importer

import (
	"errors"
	"fmt"

	"shiftory-server/internal/schedule"
)

// ValidationError describes a user-correctable workbook value without tying
// the importer to an HTTP or UI representation.
type ValidationError struct {
	Row     int
	Fields  []string
	Code    string
	Message string
	Hint    string
	Cause   error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Row > 0 {
		return fmt.Sprintf("第 %d 行：%s", e.Row, e.Message)
	}
	return e.Message
}

func (e *ValidationError) Unwrap() error { return e.Cause }

func rowValidationError(row int, fields []string, code, message, hint string, cause error) error {
	return &ValidationError{Row: row, Fields: fields, Code: code, Message: message, Hint: hint, Cause: cause}
}

func scheduleValidationDetails(err error) (code, message, hint string) {
	switch {
	case errors.Is(err, schedule.ErrOverlappingSegments):
		return "INVALID_SCHEDULE", "该日期的排班组合无效", "同一日期的时间段不能重叠，请调整开始和结束时间"
	case errors.Is(err, schedule.ErrCrossDayRequired):
		return "INVALID_CROSS_DAY", "该日期的跨日设置与时间不一致", "结束时间早于或等于开始时间时，请将是否跨日设为“是”"
	case errors.Is(err, schedule.ErrInvalidCrossDay):
		return "INVALID_CROSS_DAY", "该日期的跨日时间段无效", "跨日时间段的结束时间必须早于开始时间，且总时长不能超过 24 小时"
	case errors.Is(err, schedule.ErrIncompleteTimeRange):
		return "INCOMPLETE_TIME_RANGE", "开始时间和结束时间必须同时填写", "请同时填写开始时间和结束时间"
	case errors.Is(err, schedule.ErrRestHasSegments):
		return "REST_HAS_SEGMENTS", "休息日不能填写班次、开始时间、结束时间或跨日", "请清空班次、开始时间和结束时间，并将是否跨日设为“否”"
	default:
		return "INVALID_SCHEDULE", "该日期的排班组合无效", "请检查状态、班次、开始时间、结束时间和是否跨日的组合"
	}
}
