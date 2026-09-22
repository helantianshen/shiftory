package schedule

import (
	"errors"
	"testing"
)

// TestDayValidateWorkingTimeRange 验证合法工作时间段通过日排班校验
func TestDayValidateWorkingTimeRange(t *testing.T) {
	day := Day{
		WorkDate: MustDate("2026-09-04"),
		Status:   StatusWorking,
		Segments: []Segment{{
			Type:      SegmentTimeRange,
			StartTime: clockPtr(MustClock("08:30")),
			EndTime:   clockPtr(MustClock("17:30")),
		}},
	}

	if err := day.Validate(); err != nil {
		t.Fatalf("expected valid working day, got %v", err)
	}
}

// TestDayValidateAllowsWorkingWithoutDetails 验证只有工作状态的排班允许没有时间段
func TestDayValidateAllowsWorkingWithoutDetails(t *testing.T) {
	day := Day{WorkDate: MustDate("2026-09-04"), Status: StatusWorking}
	if err := day.Validate(); err != nil {
		t.Fatalf("working without segments is valid: %v", err)
	}
}

// TestDayValidateRejectsRestWithSegments 验证休息日包含时间段时被拒绝
func TestDayValidateRejectsRestWithSegments(t *testing.T) {
	day := Day{
		WorkDate: MustDate("2026-09-04"),
		Status:   StatusRest,
		Segments: []Segment{{Type: SegmentShift, ShiftID: uint64Ptr(1), ShiftName: "早班"}},
	}

	if err := day.Validate(); !errors.Is(err, ErrRestHasSegments) {
		t.Fatalf("expected ErrRestHasSegments, got %v", err)
	}
}

// TestSegmentValidateRequiresBothTimes 验证开始与结束时间必须成对提供
func TestSegmentValidateRequiresBothTimes(t *testing.T) {
	segment := Segment{Type: SegmentTimeRange, StartTime: clockPtr(MustClock("08:30"))}
	if err := segment.Validate(); !errors.Is(err, ErrIncompleteTimeRange) {
		t.Fatalf("expected ErrIncompleteTimeRange, got %v", err)
	}
}

// TestSegmentValidateRequiresExplicitCrossDay 验证结束不晚于开始时必须明确跨日
func TestSegmentValidateRequiresExplicitCrossDay(t *testing.T) {
	segment := Segment{
		Type:      SegmentTimeRange,
		StartTime: clockPtr(MustClock("20:00")),
		EndTime:   clockPtr(MustClock("08:00")),
	}
	if err := segment.Validate(); !errors.Is(err, ErrCrossDayRequired) {
		t.Fatalf("expected ErrCrossDayRequired, got %v", err)
	}

	segment.CrossDay = true
	if err := segment.Validate(); err != nil {
		t.Fatalf("explicit cross day should pass: %v", err)
	}
}

// TestDayValidateRejectsOverlappingSegments 验证同一天重叠时间段被拒绝
func TestDayValidateRejectsOverlappingSegments(t *testing.T) {
	day := Day{
		WorkDate: MustDate("2026-09-04"),
		Status:   StatusWorking,
		Segments: []Segment{
			{Type: SegmentTimeRange, StartTime: clockPtr(MustClock("08:30")), EndTime: clockPtr(MustClock("12:00"))},
			{Type: SegmentTimeRange, StartTime: clockPtr(MustClock("11:30")), EndTime: clockPtr(MustClock("17:30"))},
		},
	}

	if err := day.Validate(); !errors.Is(err, ErrOverlappingSegments) {
		t.Fatalf("expected ErrOverlappingSegments, got %v", err)
	}
}

// TestShiftSegmentRequiresSnapshot 验证引用班次的时间段必须保留所需快照
func TestShiftSegmentRequiresSnapshot(t *testing.T) {
	segment := Segment{Type: SegmentShift, ShiftID: uint64Ptr(1)}
	if err := segment.Validate(); !errors.Is(err, ErrShiftSnapshotRequired) {
		t.Fatalf("expected ErrShiftSnapshotRequired, got %v", err)
	}

	segment.ShiftName = "早班"
	if err := segment.Validate(); err != nil {
		t.Fatalf("shift with name snapshot should pass: %v", err)
	}
}

// TestDateAndClockRejectNonCanonicalInput 验证日期和时刻拒绝非规范输入
func TestDateAndClockRejectNonCanonicalInput(t *testing.T) {
	if _, err := ParseDate("2026-9-4"); err == nil {
		t.Fatal("expected non-canonical date to fail")
	}
	if _, err := ParseClock("8:30"); err == nil {
		t.Fatal("expected non-canonical clock to fail")
	}
}

// clockPtr 为测试构造有效的可选时刻值
func clockPtr(value Clock) *Clock { return &value }

// uint64Ptr 为测试构造可选整数 ID
func uint64Ptr(value uint64) *uint64 { return &value }
