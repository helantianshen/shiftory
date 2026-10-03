package imageai

import (
	"shiftory-server/internal/schedule"
	"testing"
)

func TestRulesExpandCalendarExceptionsConflictsAndMissing(t *testing.T) {
	start, end := schedule.MustDate("2028-02-28"), schedule.MustDate("2028-03-03")
	r := RuleSet{SchemaVersion: RuleSchemaVersion, Period: Period{start.String(), end.String()}, Issues: []Issue{}, Rules: []Rule{
		{ID: "weekday", Type: "WEEKLY", Status: "REST", Segments: []Segment{}, Weekdays: []int{1}},
		{ID: "range", Type: "DATE_RANGE", Status: "REST", Segments: []Segment{}, Start: "2028-02-29", End: "2028-03-01"},
		{ID: "exception", Type: "DATE", Status: "WORKING", Segments: []Segment{{Type: "TIME_RANGE", StartTime: "22:00", EndTime: "06:00", CrossDay: true}}, Date: "2028-02-29", Replaces: []string{"range"}},
		{ID: "conflict", Type: "DATE", Status: "REST", Segments: []Segment{}, Date: "2028-03-01"},
		{ID: "conflict2", Type: "DATE", Status: "WORKING", Segments: []Segment{}, Date: "2028-03-01"},
	}}
	draft, err := r.Expand(start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Entries) != 3 || draft.Entries[1].Date != "2028-02-29" || !draft.Entries[1].Segments[0].CrossDay || !draft.Entries[2].Uncertain {
		t.Fatalf("unexpected calendar: %+v", draft)
	}
	r.Rules[4].Replaces = []string{"conflict"}
	draft, err = r.Expand(start, end)
	if err != nil || draft.Entries[2].Uncertain {
		t.Fatal("explicit replacement failed", err)
	}
	r.Rules[3].Replaces = []string{"conflict2"}
	if _, err = r.Expand(start, end); err == nil {
		t.Fatal("replacement cycle accepted")
	}
}
func TestCycleCrossYearAndAnchorValidation(t *testing.T) {
	a, b := schedule.MustDate("2026-12-31"), schedule.MustDate("2027-01-03")
	r := RuleSet{SchemaVersion: RuleSchemaVersion, Period: Period{a.String(), b.String()}, Issues: []Issue{}, Rules: []Rule{{ID: "cycle", Type: "CYCLE", Status: "REST", Segments: []Segment{}, AnchorDate: "2026-12-30", CycleDays: 3, DayOffsets: []int{0, 1}}}}
	draft, err := r.Expand(a, b)
	if err != nil || len(draft.Entries) != 3 || draft.Entries[1].Date != "2027-01-02" {
		t.Fatal("cycle calendar failed", draft, err)
	}
	r.Rules[0].AnchorDate = ""
	if _, err = r.Expand(a, b); err == nil {
		t.Fatal("missing anchor accepted")
	}
}
func TestFenceCompatibilityHasStrictBoundaries(t *testing.T) {
	if _, err := StripJSONFence("```json\n{\"x\":1}\n```"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"note\n```json\n{}\n```", "```json\n{}\n```\nextra", "```json\n{bad}\n```", "{} {}"} {
		if _, err := StripJSONFence(input); err == nil {
			t.Fatal("invalid boundary accepted")
		}
	}
}

func TestLowPriorityRuleCannotReplaceDateException(t *testing.T) {
	date := schedule.MustDate("2026-10-01")
	r := RuleSet{SchemaVersion: RuleSchemaVersion, Period: Period{date.String(), date.String()}, Issues: []Issue{}, Rules: []Rule{
		{ID: "weekly", Type: "WEEKLY", Status: "REST", Segments: []Segment{}, Weekdays: []int{4}, Replaces: []string{"date"}},
		{ID: "date", Type: "DATE", Date: date.String(), Status: "REST", Segments: []Segment{}},
	}}
	if _, err := r.Expand(date, date); err == nil {
		t.Fatal("weekly rule replaced date exception")
	}
}
