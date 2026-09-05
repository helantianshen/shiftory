package schedule

import (
	"errors"
	"testing"
)

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

func TestDayValidateAllowsWorkingWithoutDetails(t *testing.T) {
	day := Day{WorkDate: MustDate("2026-09-04"), Status: StatusWorking}
	if err := day.Validate(); err != nil {
		t.Fatalf("working without segments is valid: %v", err)
	}
}

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

func TestSegmentValidateRequiresBothTimes(t *testing.T) {
	segment := Segment{Type: SegmentTimeRange, StartTime: clockPtr(MustClock("08:30"))}
	if err := segment.Validate(); !errors.Is(err, ErrIncompleteTimeRange) {
		t.Fatalf("expected ErrIncompleteTimeRange, got %v", err)
	}
}

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

func TestDateAndClockRejectNonCanonicalInput(t *testing.T) {
	if _, err := ParseDate("2026-9-4"); err == nil {
		t.Fatal("expected non-canonical date to fail")
	}
	if _, err := ParseClock("8:30"); err == nil {
		t.Fatal("expected non-canonical clock to fail")
	}
}

func clockPtr(value Clock) *Clock    { return &value }
func uint64Ptr(value uint64) *uint64 { return &value }
