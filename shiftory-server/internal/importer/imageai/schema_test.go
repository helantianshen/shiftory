package imageai

import (
	"strings"
	"testing"

	"shiftory-server/internal/schedule"
)

func TestDecodeDraftRejectsUnknownFieldsAndOutOfRangeDates(t *testing.T) {
	_, err := DecodeDraft(strings.NewReader(`{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[],"unexpected":true}`), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30"))
	if err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
	_, err = DecodeDraft(strings.NewReader(`{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[{"date":"2026-10-01","status":"REST","segments":[],"uncertain":false,"issues":[]}]}`), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30"))
	if err == nil {
		t.Fatal("expected out-of-range entry to be rejected")
	}
}

func TestDecodeDraftValidatesCanonicalScheduleAndUncertainty(t *testing.T) {
	draft, err := DecodeDraft(strings.NewReader(`{
      "period":{"start":"2026-09-01","end":"2026-09-30"},
      "entries":[
        {"date":"2026-09-01","status":"WORKING","segments":[{"type":"SHIFT","originalLabel":"早","mappedShiftCode":"MORNING","startTime":"08:00","endTime":"16:00","crossDay":false}],"uncertain":false,"issues":[]},
        {"date":"2026-09-02","status":"REST","segments":[],"uncertain":true,"issues":[{"field":"status","message":"图像模糊"}]}
      ]
    }`), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30"))
	if err != nil {
		t.Fatalf("decode valid draft: %v", err)
	}
	if len(draft.Entries) != 2 || !draft.Entries[1].Uncertain || draft.Entries[0].Segments[0].MappedShiftCode != "MORNING" {
		t.Fatalf("unexpected draft: %+v", draft)
	}
}

func TestDecodeDraftRejectsDuplicateDatesAndInvalidRestSegments(t *testing.T) {
	tests := []string{
		`{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[{"date":"2026-09-01","status":"REST","segments":[],"uncertain":false,"issues":[]},{"date":"2026-09-01","status":"REST","segments":[],"uncertain":false,"issues":[]}]}`,
		`{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[{"date":"2026-09-01","status":"REST","segments":[{"type":"TIME_RANGE","startTime":"08:00","endTime":"09:00","crossDay":false}],"uncertain":false,"issues":[]}]}`,
	}
	for _, input := range tests {
		if _, err := DecodeDraft(strings.NewReader(input), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30")); err == nil {
			t.Fatalf("expected invalid draft to fail: %s", input)
		}
	}
}
