package calendar

import "shiftory-server/internal/schedule"

type MemberStatus string

const MemberMissing MemberStatus = "MISSING"

type MemberDetail struct {
	UserID         uint64
	Status         MemberStatus
	Note           string
	SourceType     schedule.SourceType
	SourceImportID *uint64
	Version        uint64
	Segments       []schedule.Segment
}

type DaySummary struct {
	Date    schedule.Date
	Working int
	Rest    int
	Missing int
	AllRest bool
	Members []MemberDetail
}

func Aggregate(dates []schedule.Date, memberIDs []uint64, days []schedule.Day) []DaySummary {
	byDateAndMember := make(map[schedule.Date]map[uint64]schedule.Day, len(dates))
	for _, day := range days {
		members := byDateAndMember[day.WorkDate]
		if members == nil {
			members = make(map[uint64]schedule.Day)
			byDateAndMember[day.WorkDate] = members
		}
		members[day.UserID] = day
	}

	result := make([]DaySummary, 0, len(dates))
	for _, date := range dates {
		summary := DaySummary{Date: date, Members: make([]MemberDetail, 0, len(memberIDs))}
		for _, memberID := range memberIDs {
			day, ok := byDateAndMember[date][memberID]
			if !ok {
				summary.Missing++
				summary.Members = append(summary.Members, MemberDetail{UserID: memberID, Status: MemberMissing})
				continue
			}
			detail := MemberDetail{UserID: memberID, Status: MemberStatus(day.Status), Note: day.Note, SourceType: day.SourceType, SourceImportID: day.SourceImportID, Version: day.Version, Segments: day.Segments}
			summary.Members = append(summary.Members, detail)
			switch day.Status {
			case schedule.StatusWorking:
				summary.Working++
			case schedule.StatusRest:
				summary.Rest++
			}
		}
		summary.AllRest = len(memberIDs) > 0 && summary.Rest == len(memberIDs) && summary.Missing == 0
		result = append(result, summary)
	}
	return result
}
