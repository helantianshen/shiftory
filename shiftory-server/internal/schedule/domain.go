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

type Date string

func ParseDate(value string) (Date, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return "", ErrInvalidDate
	}
	return Date(value), nil
}

func MustDate(value string) Date {
	date, err := ParseDate(value)
	if err != nil {
		panic(err)
	}
	return date
}

func (d Date) String() string { return string(d) }

type Clock uint16

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

func MustClock(value string) Clock {
	clock, err := ParseClock(value)
	if err != nil {
		panic(err)
	}
	return clock
}

func (c Clock) String() string {
	minutes := int(c)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

type Status string

const (
	StatusWorking Status = "WORKING"
	StatusRest    Status = "REST"
)

type SourceType string

const (
	SourceManual  SourceType = "MANUAL"
	SourceXLSX    SourceType = "XLSX"
	SourceXLS     SourceType = "XLS"
	SourceImageAI SourceType = "IMAGE_AI"
)

type SegmentType string

const (
	SegmentShift     SegmentType = "SHIFT"
	SegmentTimeRange SegmentType = "TIME_RANGE"
)

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

	if (s.StartTime == nil) != (s.EndTime == nil) {
		return ErrIncompleteTimeRange
	}
	if s.Type == SegmentTimeRange && s.StartTime == nil {
		return ErrIncompleteTimeRange
	}
	if s.StartTime == nil {
		return nil
	}

	start, end := int(*s.StartTime), int(*s.EndTime)
	if !s.CrossDay && end <= start {
		return ErrCrossDayRequired
	}
	if s.CrossDay && end > start {
		return ErrInvalidCrossDay
	}
	return nil
}

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
		if segment.CrossDay {
			end += 24 * 60
		}
		intervals = append(intervals, interval{start: start, end: end})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	for i := 1; i < len(intervals); i++ {
		if intervals[i].start < intervals[i-1].end {
			return ErrOverlappingSegments
		}
	}
	return nil
}
