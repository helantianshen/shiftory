// Package calendar 按选定成员的正式日排班计算工作、休息、缺失与全部休息状态
package calendar

import "shiftory-server/internal/schedule"

// MemberStatus 表示日历成员状态，允许正式状态之外的缺失标记
type MemberStatus string

const MemberMissing MemberStatus = "MISSING"

// MemberDetail 保存日历中单个成员的已确认排班详情
type MemberDetail struct {
	UserID         uint64
	Status         MemberStatus
	Note           string
	SourceType     schedule.SourceType
	SourceImportID *uint64
	Version        uint64
	Segments       []schedule.Segment
}

// DaySummary 保存选定成员集合在某日的统计与成员明细
type DaySummary struct {
	Date    schedule.Date
	Working int
	Rest    int
	Missing int
	AllRest bool
	Members []MemberDetail
}

// Aggregate 按指定成员集合汇总日期，跨日排班仍只归属其开始日期
// AllRest 仅在集合非空且每名成员均明确休息时成立，缺失排班不视为休息
func Aggregate(dates []schedule.Date, memberIDs []uint64, days []schedule.Day) []DaySummary {
	// 以排班开始日期和用户建立索引，跨日时间段不会产生次日副本
	byDateAndMember := make(map[schedule.Date]map[uint64]schedule.Day, len(dates))
	for _, day := range days {
		members := byDateAndMember[day.WorkDate]
		if members == nil {
			members = make(map[uint64]schedule.Day)
			byDateAndMember[day.WorkDate] = members
		}
		members[day.UserID] = day
	}

	// 按传入日期与成员顺序输出详情，没有正式记录的成员计入缺失
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
