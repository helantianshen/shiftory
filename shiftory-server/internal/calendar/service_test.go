package calendar

import (
	"testing"

	"shiftory-server/internal/schedule"
)

func TestAggregateDistinguishesRestWorkingAndMissing(t *testing.T) {
	date := schedule.MustDate("2026-09-04")
	result := Aggregate(
		[]schedule.Date{date},
		[]uint64{1, 2, 3},
		[]schedule.Day{
			{UserID: 1, WorkDate: date, Status: schedule.StatusWorking},
			{UserID: 2, WorkDate: date, Status: schedule.StatusRest},
		},
	)

	if len(result) != 1 {
		t.Fatalf("expected one summary, got %d", len(result))
	}
	summary := result[0]
	if summary.Working != 1 || summary.Rest != 1 || summary.Missing != 1 || summary.AllRest {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestAggregateMarksAllRestOnlyWhenEverySelectedMemberExplicitlyRests(t *testing.T) {
	date := schedule.MustDate("2026-09-04")
	result := Aggregate(
		[]schedule.Date{date},
		[]uint64{1, 2},
		[]schedule.Day{
			{UserID: 1, WorkDate: date, Status: schedule.StatusRest},
			{UserID: 2, WorkDate: date, Status: schedule.StatusRest},
		},
	)
	if !result[0].AllRest {
		t.Fatalf("expected all rest: %+v", result[0])
	}
}

func TestAggregateNeverMarksEmptySelectionAllRest(t *testing.T) {
	date := schedule.MustDate("2026-09-04")
	result := Aggregate([]schedule.Date{date}, nil, nil)
	if result[0].AllRest || result[0].Missing != 0 {
		t.Fatalf("empty selection must be an empty summary: %+v", result[0])
	}
}

func TestAggregateCrossDayBelongsOnlyToStartDate(t *testing.T) {
	start := schedule.MustDate("2026-09-04")
	next := schedule.MustDate("2026-09-05")
	result := Aggregate(
		[]schedule.Date{start, next},
		[]uint64{1},
		[]schedule.Day{{
			UserID:   1,
			WorkDate: start,
			Status:   schedule.StatusWorking,
			Segments: []schedule.Segment{{
				Type: schedule.SegmentTimeRange, StartTime: clock(schedule.MustClock("20:00")),
				EndTime: clock(schedule.MustClock("08:00")), CrossDay: true,
			}},
		}},
	)
	if result[0].Working != 1 || result[1].Working != 0 || result[1].Missing != 1 {
		t.Fatalf("cross-day ownership is incorrect: %+v", result)
	}
}

func clock(value schedule.Clock) *schedule.Clock { return &value }
