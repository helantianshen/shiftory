package imageai

import (
	"strings"
	"testing"

	"shiftory-server/internal/schedule"
)

// TestDecodeDraftRejectsUnknownFieldsAndOutOfRangeDates 验证草稿拒绝未知字段与越界日期
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

// TestDecodeDraftValidatesCanonicalScheduleAndUncertainty 验证规范排班及不确定条目的问题说明
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

// TestDecodeDraftAcceptsProviderSchedulesAlias 验证网关 schedules 别名可归一化为 entries
func TestDecodeDraftAcceptsProviderSchedulesAlias(t *testing.T) {
	draft, err := DecodeDraft(strings.NewReader(`{
      "period":{"start":"2026-09-01","end":"2026-09-30"},
      "schedules":[{"date":"2026-09-01","status":"REST","segments":[],"uncertain":false,"issues":[]}]
    }`), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30"))
	if err != nil {
		t.Fatalf("decode provider schedules alias: %v", err)
	}
	if len(draft.Entries) != 1 || draft.Entries[0].Date != "2026-09-01" {
		t.Fatalf("unexpected aliased entries: %+v", draft.Entries)
	}
}

// TestDecodeDraftAcceptsProviderShiftsAndTopLevelIssues 验证 shifts 别名及顶层问题说明的兼容读取
func TestDecodeDraftAcceptsProviderShiftsAndTopLevelIssues(t *testing.T) {
	draft, err := DecodeDraft(strings.NewReader(`{
      "period":{"start":"2026-09-01","end":"2026-09-30"},
      "schedules":[{"date":"2026-09-01","status":"WORKING","shifts":[{"type":"SHIFT","originalLabel":"早班","mappedShiftCode":"MORNING","startTime":"08:00","endTime":"16:00","crossDay":false}],"uncertain":false,"issues":[]}],
      "issues":[]
    }`), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30"))
	if err != nil {
		t.Fatalf("decode provider shifts alias: %v", err)
	}
	if len(draft.Entries) != 1 || len(draft.Entries[0].Segments) != 1 || draft.Entries[0].Segments[0].MappedShiftCode != "MORNING" {
		t.Fatalf("unexpected aliased schedule: %+v", draft.Entries)
	}
}

// TestDecodeDraftRejectsDuplicateDates 验证重复日期被拒绝
func TestDecodeDraftRejectsDuplicateDates(t *testing.T) {
	tests := []string{
		`{"period":{"start":"2026-09-01","end":"2026-09-30"},"entries":[{"date":"2026-09-01","status":"REST","segments":[],"uncertain":false,"issues":[]},{"date":"2026-09-01","status":"REST","segments":[],"uncertain":false,"issues":[]}]}`,
	}
	for _, input := range tests {
		if _, err := DecodeDraft(strings.NewReader(input), schedule.MustDate("2026-09-01"), schedule.MustDate("2026-09-30")); err == nil {
			t.Fatalf("expected invalid draft to fail: %s", input)
		}
	}
}
