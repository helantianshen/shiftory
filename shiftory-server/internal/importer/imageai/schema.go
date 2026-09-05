package imageai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"shiftory-server/internal/schedule"
)

const (
	PromptVersion = "2026-09-04.v1"
	SchemaVersion = "shiftory.image-schedule.v1"
)

type Period struct {
	Start string `json:"start" jsonschema:"description=First represented date in YYYY-MM-DD"`
	End   string `json:"end" jsonschema:"description=Last represented date in YYYY-MM-DD"`
}

type Issue struct {
	Field   string `json:"field" jsonschema:"description=JSON field path that is uncertain"`
	Message string `json:"message" jsonschema:"description=Short reason for uncertainty"`
}

type Segment struct {
	Type            string `json:"type" jsonschema:"enum=SHIFT,enum=TIME_RANGE"`
	OriginalLabel   string `json:"originalLabel"`
	MappedShiftCode string `json:"mappedShiftCode"`
	StartTime       string `json:"startTime" jsonschema:"description=HH:mm or empty"`
	EndTime         string `json:"endTime" jsonschema:"description=HH:mm or empty"`
	CrossDay        bool   `json:"crossDay"`
}

type Entry struct {
	Date      string    `json:"date" jsonschema:"description=Canonical YYYY-MM-DD"`
	Status    string    `json:"status" jsonschema:"enum=WORKING,enum=REST"`
	Note      string    `json:"note"`
	Segments  []Segment `json:"segments"`
	Uncertain bool      `json:"uncertain"`
	Issues    []Issue   `json:"issues"`
}

type Draft struct {
	Period  Period  `json:"period"`
	Entries []Entry `json:"entries"`
}

func DecodeDraft(reader io.Reader, requestedStart, requestedEnd schedule.Date) (Draft, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 2<<20))
	decoder.DisallowUnknownFields()
	var draft Draft
	if err := decoder.Decode(&draft); err != nil {
		return Draft{}, fmt.Errorf("decode image schedule draft: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Draft{}, err
	}
	if err := draft.Validate(requestedStart, requestedEnd); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("image schedule draft contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing image schedule data: %w", err)
	}
	return nil
}

func (d Draft) Validate(requestedStart, requestedEnd schedule.Date) error {
	periodStart, startErr := schedule.ParseDate(d.Period.Start)
	periodEnd, endErr := schedule.ParseDate(d.Period.End)
	if startErr != nil || endErr != nil || periodStart.String() > periodEnd.String() {
		return errors.New("image schedule period is invalid")
	}
	if periodStart.String() < requestedStart.String() || periodEnd.String() > requestedEnd.String() {
		return errors.New("image schedule period exceeds requested range")
	}
	seen := make(map[schedule.Date]bool, len(d.Entries))
	for index, entry := range d.Entries {
		date, err := schedule.ParseDate(entry.Date)
		if err != nil || date.String() < requestedStart.String() || date.String() > requestedEnd.String() {
			return fmt.Errorf("entry %d has an invalid or out-of-range date", index)
		}
		if seen[date] {
			return fmt.Errorf("entry %d duplicates date %s", index, date)
		}
		seen[date] = true
		if entry.Status != string(schedule.StatusWorking) && entry.Status != string(schedule.StatusRest) {
			return fmt.Errorf("entry %d has invalid status", index)
		}
		if entry.Status == string(schedule.StatusRest) && len(entry.Segments) != 0 {
			return fmt.Errorf("entry %d: %w", index, schedule.ErrRestHasSegments)
		}
		if entry.Uncertain && len(entry.Issues) == 0 {
			return fmt.Errorf("entry %d is uncertain but has no issues", index)
		}
		for segmentIndex, segment := range entry.Segments {
			if err := validateSegment(segment); err != nil {
				return fmt.Errorf("entry %d segment %d: %w", index, segmentIndex, err)
			}
		}
	}
	return nil
}

func validateSegment(segment Segment) error {
	switch segment.Type {
	case string(schedule.SegmentShift):
		if strings.TrimSpace(segment.OriginalLabel) == "" && strings.TrimSpace(segment.MappedShiftCode) == "" {
			return errors.New("shift segment has no recognizable label")
		}
	case string(schedule.SegmentTimeRange):
		if segment.StartTime == "" || segment.EndTime == "" {
			return schedule.ErrIncompleteTimeRange
		}
	default:
		return schedule.ErrInvalidSegmentType
	}
	if (segment.StartTime == "") != (segment.EndTime == "") {
		return schedule.ErrIncompleteTimeRange
	}
	if segment.StartTime != "" {
		start, startErr := schedule.ParseClock(segment.StartTime)
		end, endErr := schedule.ParseClock(segment.EndTime)
		if startErr != nil || endErr != nil {
			return schedule.ErrInvalidClock
		}
		if !segment.CrossDay && end <= start {
			return schedule.ErrCrossDayRequired
		}
		if segment.CrossDay && end > start {
			return schedule.ErrInvalidCrossDay
		}
	}
	return nil
}
