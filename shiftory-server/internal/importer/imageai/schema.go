// Package imageai 封装视觉模型调用、兼容字段归一化与排班草稿校验，不写入正式排班
package imageai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"shiftory-server/internal/schedule"
)

const (
	PromptVersion = "2026-10-02.v2"
	SchemaVersion = "shiftory.image-schedule.v1"
)

// Period 表示模型返回草稿覆盖的日期区间
type Period struct {
	Start string `json:"start" jsonschema:"description=First represented date in YYYY-MM-DD"`
	End   string `json:"end" jsonschema:"description=Last represented date in YYYY-MM-DD"`
}

// Issue 描述模型无法可靠识别的字段及原因
type Issue struct {
	Field   string `json:"field" jsonschema:"description=JSON field path that is uncertain"`
	Message string `json:"message" jsonschema:"description=Short reason for uncertainty"`
}

// Segment 表示模型识别的班次标签或时间段，尚未映射为工作区班次快照
type Segment struct {
	Type            string `json:"type" jsonschema:"enum=SHIFT,enum=TIME_RANGE"`
	OriginalLabel   string `json:"originalLabel"`
	MappedShiftCode string `json:"mappedShiftCode"`
	StartTime       string `json:"startTime" jsonschema:"description=HH:mm or empty"`
	EndTime         string `json:"endTime" jsonschema:"description=HH:mm or empty"`
	CrossDay        bool   `json:"crossDay"`
}

// Entry 表示模型识别的单日状态、时间段及字段级不确定信息
type Entry struct {
	RuleIDs   []string  `json:"ruleIds,omitempty"`
	Date      string    `json:"date" jsonschema:"description=Canonical YYYY-MM-DD"`
	Status    string    `json:"status" jsonschema:"enum=WORKING,enum=REST"`
	Note      string    `json:"note"`
	Segments  []Segment `json:"segments"`
	Uncertain bool      `json:"uncertain"`
	Issues    []Issue   `json:"issues"`
}

// Draft 承载模型结构化草稿，仍需业务校验与人工确认
type Call struct {
	FinishReason string
	OutputTokens int
	ProviderID   string
	Model        string
	StartedAt    time.Time
	FinishedAt   time.Time
	HTTPStatus   int
	Code         string
	Raw          string
	Normalized   []byte
}

type Draft struct {
	RawResponse       string  `json:"-"`
	ModelName         string  `json:"-"`
	ProviderID        string  `json:"-"`
	ConfigFingerprint string  `json:"-"`
	RulesSnapshot     []byte  `json:"-"`
	Period            Period  `json:"period"`
	Entries           []Entry `json:"entries"`
	Issues            []Issue `json:"issues,omitempty"`
}

// DecodeDraft 归一化已知模型输出别名后严格解码，并校验请求日期范围
func DecodeDraft(reader io.Reader, requestedStart, requestedEnd schedule.Date) (Draft, error) {
	// 先归一化已知网关别名，再以严格 Schema 拒绝未知字段和越界业务数据
	decoder := json.NewDecoder(io.LimitReader(reader, 2<<20))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return Draft{}, fmt.Errorf("decode image schedule draft: %w", err)
	}
	normalized, err := normalizeProviderPayload(payload)
	if err != nil {
		return Draft{}, fmt.Errorf("decode image schedule draft: %w", err)
	}
	strictDecoder := json.NewDecoder(bytes.NewReader(normalized))
	strictDecoder.DisallowUnknownFields()
	var draft Draft
	if err := strictDecoder.Decode(&draft); err != nil {
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

// normalizeProviderPayload 归一化已知字段和枚举别名，保留未知字段供严格解码拒绝
func normalizeProviderPayload(payload json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, err
	}
	// 同义字段同时出现时拒绝选择，防止两份排班数据相互覆盖
	_, hasEntries := fields["entries"]
	schedules, hasSchedules := fields["schedules"]
	if hasEntries && hasSchedules {
		return nil, errors.New("image schedule draft contains both entries and schedules")
	}
	if !hasEntries && hasSchedules {
		fields["entries"] = schedules
		delete(fields, "schedules")
	}
	if entries, ok := fields["entries"]; ok {
		var rawEntries []json.RawMessage
		if err := json.Unmarshal(entries, &rawEntries); err != nil {
			return nil, err
		}
		for index, rawEntry := range rawEntries {
			var entryFields map[string]json.RawMessage
			if err := json.Unmarshal(rawEntry, &entryFields); err != nil {
				return nil, fmt.Errorf("invalid schedule entry %d: %w", index, err)
			}
			_, hasSegments := entryFields["segments"]
			shifts, hasShifts := entryFields["shifts"]
			if hasSegments && hasShifts {
				return nil, fmt.Errorf("schedule entry %d contains both segments and shifts", index)
			}
			if !hasSegments && hasShifts {
				entryFields["segments"] = shifts
				delete(entryFields, "shifts")
			}
			// 兼容网关可能改变枚举大小写或用 custom 表示自由时间段，严格解析前需归一化这些别名
			if status, ok := entryFields["status"]; ok {
				var value string
				if json.Unmarshal(status, &value) == nil {
					value = strings.ToUpper(strings.TrimSpace(value))
					if value == "WORK" {
						value = "WORKING"
					}
					if value == "OFF" {
						value = "REST"
					}
					entryFields["status"] = json.RawMessage(strconv.Quote(value))
				}
			}
			if segments, ok := entryFields["segments"]; ok {
				var rawSegments []json.RawMessage
				if err := json.Unmarshal(segments, &rawSegments); err != nil {
					return nil, err
				}
				for i, rawSegment := range rawSegments {
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(rawSegment, &fields); err != nil {
						return nil, err
					}
					if typ, ok := fields["type"]; ok {
						var value string
						if json.Unmarshal(typ, &value) == nil {
							value = strings.ToUpper(strings.TrimSpace(value))
							if value == "CUSTOM" || value == "TIME" || value == "RANGE" {
								value = "TIME_RANGE"
							}
							fields["type"] = json.RawMessage(strconv.Quote(value))
						}
					}
					// 模型的 24:00 表达转换为午夜并显式标记跨日，供领域时间校验处理
					for _, key := range []string{"endTime"} {
						if value, ok := fields[key]; ok {
							var clock string
							if json.Unmarshal(value, &clock) == nil && strings.TrimSpace(clock) == "24:00" {
								fields[key] = json.RawMessage(`"00:00"`)
								fields["crossDay"] = json.RawMessage(`true`)
							}
						}
					}
					normalized, err := json.Marshal(fields)
					if err != nil {
						return nil, err
					}
					rawSegments[i] = normalized
				}
				normalized, err := json.Marshal(rawSegments)
				if err != nil {
					return nil, err
				}
				entryFields["segments"] = normalized
			}
			normalizedEntry, err := json.Marshal(entryFields)
			if err != nil {
				return nil, err
			}
			rawEntries[index] = normalizedEntry
		}
		normalizedEntries, err := json.Marshal(rawEntries)
		if err != nil {
			return nil, err
		}
		fields["entries"] = normalizedEntries
	}
	return json.Marshal(fields)
}

// ensureJSONEOF 确保 JSON 值后只有空白，拒绝额外值或非法尾部内容
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

// Validate 校验识别周期、日期唯一性、状态与逐段规则，不执行班次映射
func (d Draft) Validate(requestedStart, requestedEnd schedule.Date) error {
	periodStart, startErr := schedule.ParseDate(d.Period.Start)
	periodEnd, endErr := schedule.ParseDate(d.Period.End)
	if startErr != nil || endErr != nil || periodStart.String() > periodEnd.String() {
		return errors.New("image schedule period is invalid")
	}
	if periodStart.String() < requestedStart.String() || periodEnd.String() > requestedEnd.String() {
		return errors.New("image schedule period exceeds requested range")
	}
	// 逐日检查请求边界与重复日期，不确定条目必须携带可展示的问题说明
	if d.Entries == nil || len(d.Entries) > 366 {
		return errors.New("entries must be a bounded array")
	}
	seen := make(map[schedule.Date]bool, len(d.Entries))
	for index, entry := range d.Entries {
		date, err := schedule.ParseDate(entry.Date)
		if err != nil || date.String() < requestedStart.String() || date.String() > requestedEnd.String() {
			return fmt.Errorf("entry %d has an invalid or out-of-range date", index)
		}
		if entry.Segments == nil {
			return errors.New("segments must be an array")
		}
		if seen[date] {
			return fmt.Errorf("entry %d duplicates date %s", index, date)
		}
		seen[date] = true
		if entry.Status != string(schedule.StatusWorking) && entry.Status != string(schedule.StatusRest) {
			return fmt.Errorf("entry %d has invalid status", index)
		}

		if entry.Uncertain && len(entry.Issues) == 0 {
			return fmt.Errorf("entry %d is uncertain but has no issues", index)
		}
		for segmentIndex, segment := range entry.Segments {
			if segment.Type != "SHIFT" && segment.Type != "TIME_RANGE" {
				return fmt.Errorf("entry %d segment %d has invalid type", index, segmentIndex)
			}
		}
	}
	return nil
}

// validateSegment 校验模型时间段的可识别标签、完整时间及跨日语义
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
