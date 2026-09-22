// Package schedule 定义日排班、班次快照与时间段的领域规则，不负责数据库访问
package schedule

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidDate           = errors.New("invalid schedule date")
	ErrInvalidClock          = errors.New("invalid schedule clock")
	ErrInvalidStatus         = errors.New("invalid schedule status")
	ErrInvalidSegmentType    = errors.New("invalid segment type")
	ErrRestHasSegments       = errors.New("rest day cannot contain segments")
	ErrIncompleteTimeRange   = errors.New("start and end time must both be present")
	ErrCrossDayRequired      = errors.New("cross-day must be explicit when end time is not after start time")
	ErrInvalidCrossDay       = errors.New("cross-day time range exceeds 24 hours")
	ErrOverlappingSegments   = errors.New("schedule segments overlap")
	ErrShiftSnapshotRequired = errors.New("shift segment requires an id and name snapshot")
)

// Date 表示规范日期文本，不包含时区或时刻
type Date string

// ParseDate 解析规范的 YYYY-MM-DD 日期，拒绝非法日期及非规范表示
func ParseDate(value string) (Date, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return "", ErrInvalidDate
	}
	return Date(value), nil
}

// MustDate 解析必须有效的日期，输入非法时触发 panic
func MustDate(value string) Date {
	date, err := ParseDate(value)
	if err != nil {
		panic(err)
	}
	return date
}

// String 返回规范日期文本
func (d Date) String() string { return string(d) }

// Clock 表示当天零点后的分钟数，合法输入固定为 HH:mm
type Clock uint16

// ParseClock 将 HH:mm 时刻解析为从午夜起算的分钟数
func ParseClock(value string) (Clock, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, ErrInvalidClock
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, ErrInvalidClock
	}
	return Clock(parsed.Hour()*60 + parsed.Minute()), nil
}

// MustClock 解析必须有效的时刻，输入非法时触发 panic
func MustClock(value string) Clock {
	clock, err := ParseClock(value)
	if err != nil {
		panic(err)
	}
	return clock
}

// String 将分钟数格式化为两位小时和分钟
func (c Clock) String() string {
	minutes := int(c)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// Status 表示正式排班的工作或休息状态，不包含查询派生的缺失状态
type Status string

const (
	StatusWorking Status = "WORKING"
	StatusRest    Status = "REST"
)

// SourceType 标记排班来自手动编辑、表格或图片识别
type SourceType string

const (
	SourceManual  SourceType = "MANUAL"
	SourceXLSX    SourceType = "XLSX"
	SourceXLS     SourceType = "XLS"
	SourceImageAI SourceType = "IMAGE_AI"
)

// SegmentType 区分预定义班次引用与自定义时间范围
type SegmentType string

const (
	SegmentShift     SegmentType = "SHIFT"
	SegmentTimeRange SegmentType = "TIME_RANGE"
)

// Segment 保存班次快照，已生成的排班不依赖后续班次名称、时间或颜色变化
type Segment struct {
	ID            uint64
	Type          SegmentType
	ShiftID       *uint64
	ShiftName     string
	ShiftCode     string
	StartTime     *Clock
	EndTime       *Clock
	CrossDay      bool
	DisplayColor  string
	SortOrder     int
	OriginalLabel string
}

// Validate 校验时间段类型、班次快照与显式跨日约束
func (s Segment) Validate() error {
	switch s.Type {
	case SegmentShift:
		if s.ShiftID == nil || strings.TrimSpace(s.ShiftName) == "" {
			return ErrShiftSnapshotRequired
		}
	case SegmentTimeRange:
		if s.ShiftID != nil {
			return ErrInvalidSegmentType
		}
	default:
		return ErrInvalidSegmentType
	}

	// 引用班次可以没有具体时间，自定义时间段必须提供完整起止时刻
	if (s.StartTime == nil) != (s.EndTime == nil) {
		return ErrIncompleteTimeRange
	}
	if s.Type == SegmentTimeRange && s.StartTime == nil {
		return ErrIncompleteTimeRange
	}
	if s.StartTime == nil {
		return nil
	}

	// 跨日只允许结束时刻不晚于开始时刻，相同时刻在跨日模式下表示二十四小时
	start, end := int(*s.StartTime), int(*s.EndTime)
	if !s.CrossDay && end <= start {
		return ErrCrossDayRequired
	}
	if s.CrossDay && end > start {
		return ErrInvalidCrossDay
	}
	return nil
}

// Day 表示一个用户某日的正式排班，版本用于并发写入检查
type Day struct {
	ID             uint64
	WorkspaceID    uint64
	UserID         uint64
	WorkDate       Date
	Status         Status
	SourceType     SourceType
	SourceImportID *uint64
	Note           string
	Version        uint64
	CreatedBy      uint64
	Segments       []Segment
}

// Validate 校验日期与状态，并拒绝休息日时间段和同日时间重叠
func (d Day) Validate() error {
	if _, err := ParseDate(d.WorkDate.String()); err != nil {
		return err
	}
	if d.Status != StatusWorking && d.Status != StatusRest {
		return ErrInvalidStatus
	}
	if d.Status == StatusRest && len(d.Segments) > 0 {
		return ErrRestHasSegments
	}

	type interval struct{ start, end int }
	intervals := make([]interval, 0, len(d.Segments))
	for _, segment := range d.Segments {
		if err := segment.Validate(); err != nil {
			return err
		}
		if segment.StartTime == nil {
			continue
		}
		start, end := int(*segment.StartTime), int(*segment.EndTime)
		// 跨日区间展开到次日分钟轴后再参与重叠判断
		if segment.CrossDay {
			end += 24 * 60
		}
		intervals = append(intervals, interval{start: start, end: end})
	}
	// 按起点排序后只比较相邻区间，端点相接允许，存在交叠则拒绝
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	for i := 1; i < len(intervals); i++ {
		if intervals[i].start < intervals[i-1].end {
			return ErrOverlappingSegments
		}
	}
	return nil
}
