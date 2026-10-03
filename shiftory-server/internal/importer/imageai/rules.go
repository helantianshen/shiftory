package imageai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"shiftory-server/internal/schedule"
	"time"
)

const RuleSchemaVersion = "shiftory.rules.v1"

type Rule struct {
	ID         string    `json:"id"`
	Type       string    `json:"type" jsonschema:"enum=WEEKLY,enum=DATE_RANGE,enum=DATE,enum=CYCLE"`
	Status     string    `json:"status" jsonschema:"enum=WORKING,enum=REST"`
	Segments   []Segment `json:"segments"`
	Note       string    `json:"note"`
	Weekdays   []int     `json:"weekdays,omitempty"`
	Start      string    `json:"start,omitempty"`
	End        string    `json:"end,omitempty"`
	Date       string    `json:"date,omitempty"`
	AnchorDate string    `json:"anchorDate,omitempty"`
	CycleDays  int       `json:"cycleDays,omitempty"`
	DayOffsets []int     `json:"dayOffsets,omitempty"`
	Replaces   []string  `json:"replaces,omitempty"`
}
type RuleSet struct {
	SchemaVersion string  `json:"schemaVersion"`
	Period        Period  `json:"period"`
	Rules         []Rule  `json:"rules"`
	Issues        []Issue `json:"issues"`
}

func DecodeRules(content []byte, start, end schedule.Date) (RuleSet, error) {
	var rules RuleSet
	d := json.NewDecoder(bytes.NewReader(content))
	d.DisallowUnknownFields()
	if err := d.Decode(&rules); err != nil {
		return rules, errors.New("invalid rule JSON")
	}
	if err := ensureJSONEOF(d); err != nil {
		return rules, errors.New("invalid trailing rule content")
	}
	if err := rules.Validate(start, end); err != nil {
		return rules, err
	}
	return rules, nil
}
func (r RuleSet) Validate(start, end schedule.Date) error {
	if r.SchemaVersion != RuleSchemaVersion || r.Period.Start != start.String() || r.Period.End != end.String() || r.Rules == nil || r.Issues == nil {
		return errors.New("invalid rule schema or period")
	}
	ids := map[string]bool{}
	for _, rule := range r.Rules {
		if rule.ID == "" || ids[rule.ID] {
			return errors.New("duplicate or empty rule id")
		}
		ids[rule.ID] = true
		if rule.Status != "WORKING" && rule.Status != "REST" {
			return errors.New("invalid rule status")
		}
		if rule.Segments == nil {
			return errors.New("rule segments must be an array")
		}
		switch rule.Type {
		case "WEEKLY":
			if len(rule.Weekdays) == 0 || rule.Date != "" || rule.AnchorDate != "" || rule.CycleDays != 0 || len(rule.DayOffsets) > 0 {
				return errors.New("invalid weekly condition")
			}
			seen := map[int]bool{}
			for _, n := range rule.Weekdays {
				if n < 1 || n > 7 || seen[n] {
					return errors.New("invalid weekdays")
				}
				seen[n] = true
			}
		case "DATE_RANGE":
			if rule.Start == "" || rule.End == "" || rule.Date != "" || len(rule.Weekdays) > 0 || rule.AnchorDate != "" || rule.CycleDays != 0 || len(rule.DayOffsets) > 0 {
				return errors.New("invalid date range")
			}
		case "DATE":
			if _, err := schedule.ParseDate(rule.Date); err != nil || rule.Date < start.String() || rule.Date > end.String() || rule.Start != "" || rule.End != "" || len(rule.Weekdays) > 0 || rule.AnchorDate != "" || rule.CycleDays != 0 || len(rule.DayOffsets) > 0 {
				return errors.New("invalid date exception")
			}
		case "CYCLE":
			if _, err := schedule.ParseDate(rule.AnchorDate); err != nil || rule.CycleDays < 1 || rule.CycleDays > 366 || len(rule.DayOffsets) == 0 || rule.Date != "" || len(rule.Weekdays) > 0 {
				return errors.New("invalid cycle anchor")
			}
			seen := map[int]bool{}
			for _, n := range rule.DayOffsets {
				if n < 0 || n >= rule.CycleDays || seen[n] {
					return errors.New("invalid cycle offsets")
				}
				seen[n] = true
			}
		default:
			return errors.New("invalid rule type")
		}
		if rule.Start != "" || rule.End != "" {
			if _, err := schedule.ParseDate(rule.Start); err != nil {
				return err
			}
			if _, err := schedule.ParseDate(rule.End); err != nil {
				return err
			}
			if rule.Start > rule.End || rule.Start < start.String() || rule.End > end.String() {
				return errors.New("rule range exceeds period")
			}
		}
	}
	for _, rule := range r.Rules {
		for _, id := range rule.Replaces {
			if id == rule.ID || !ids[id] {
				return errors.New("invalid replacement rule")
			}
			// 替代规则不得降低日期优先级，环形关系必须拒绝
			for _, other := range r.Rules {
				if other.ID == id && rank(other.Type) > rank(rule.Type) {
					return errors.New("replacement priority mismatch")
				}
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return errors.New("cyclic rule replacements")
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, rule := range r.Rules {
			if rule.ID == id {
				for _, next := range rule.Replaces {
					if err := visit(next); err != nil {
						return err
					}
				}
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
func rank(t string) int {
	switch t {
	case "DATE":
		return 3
	case "DATE_RANGE":
		return 2
	default:
		return 1
	}
}
func (r RuleSet) Expand(start, end schedule.Date) (Draft, error) {
	if err := r.Validate(start, end); err != nil {
		return Draft{}, err
	}
	first, _ := time.Parse("2006-01-02", start.String())
	last, _ := time.Parse("2006-01-02", end.String())
	if first.After(last) || last.Sub(first) > 365*24*time.Hour {
		return Draft{}, errors.New("invalid rule period")
	}
	draft := Draft{Period: r.Period, Entries: []Entry{}, Issues: r.Issues}
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		priority := 0
		matches := []Rule{}
		for _, rule := range r.Rules {
			if !rule.matches(day) {
				continue
			}
			p := rank(rule.Type)
			if p > priority {
				matches = nil
				priority = p
			}
			if p == priority {
				matches = append(matches, rule)
			}
		}
		if len(matches) == 0 {
			continue
		}
		replaced := map[string]bool{}
		for _, rule := range matches {
			for _, id := range rule.Replaces {
				replaced[id] = true
			}
		}
		active := []Rule{}
		for _, rule := range matches {
			if !replaced[rule.ID] {
				active = append(active, rule)
			}
		}
		entry := Entry{Date: date, Status: active[0].Status, Segments: active[0].Segments, Note: active[0].Note, Issues: []Issue{}}
		for _, rule := range active {
			entry.RuleIDs = append(entry.RuleIDs, rule.ID)
			if rule.Status != entry.Status || rule.Note != entry.Note || !reflect.DeepEqual(rule.Segments, entry.Segments) {
				entry.Uncertain = true
				entry.Issues = append(entry.Issues, Issue{Field: "rules", Message: "同优先级规则存在冲突，需人工确定"})
			}
		}
		draft.Entries = append(draft.Entries, entry)
	}
	return draft, nil
}
func (r Rule) matches(day time.Time) bool {
	date := day.Format("2006-01-02")
	if r.Start != "" && (date < r.Start || date > r.End) {
		return false
	}
	switch r.Type {
	case "DATE":
		return date == r.Date
	case "DATE_RANGE":
		return true
	case "WEEKLY":
		w := int(day.Weekday())
		if w == 0 {
			w = 7
		}
		for _, n := range r.Weekdays {
			if n == w {
				return true
			}
		}
	case "CYCLE":
		anchor, _ := time.Parse("2006-01-02", r.AnchorDate)
		delta := int(day.Sub(anchor) / (24 * time.Hour))
		if delta < 0 {
			return false
		}
		for _, n := range r.DayOffsets {
			if delta%r.CycleDays == n {
				return true
			}
		}
	}
	return false
}

func StripJSONFence(content string) ([]byte, error) {
	content = string(bytes.TrimSpace([]byte(content)))
	if len(content) >= 3 && content[:3] == "```" {
		for _, prefix := range []string{"```json\n", "```\n"} {
			if len(content) >= len(prefix)+4 && content[:len(prefix)] == prefix && content[len(content)-4:] == "\n```" {
				content = content[len(prefix) : len(content)-4]
				break
			}
		}
	}
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("model output is not complete JSON")
	}
	return []byte(content), nil
}
