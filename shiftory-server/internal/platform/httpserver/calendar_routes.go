package httpserver

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	calendarservice "shiftory-server/internal/calendar"
	"shiftory-server/internal/schedule"
)

func (s *server) getCalendar(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	start, startErr := schedule.ParseDate(c.Query("start"))
	end, endErr := schedule.ParseDate(c.Query("end"))
	if startErr != nil || endErr != nil {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", "日期范围无效", nil)
		return
	}
	dates, err := dateRange(start, end, 366)
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", err.Error(), nil)
		return
	}
	memberIDs, err := parseMemberIDs(c.Query("memberIds"))
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_MEMBERS", "成员列表无效", nil)
		return
	}
	periods, err := s.memberPeriods(workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询成员有效期", nil)
		return
	}
	if len(memberIDs) == 0 {
		for _, period := range periods {
			if period.overlaps(start, end) {
				memberIDs = append(memberIDs, period.UserID)
			}
		}
	} else {
		known := make(map[uint64]bool, len(periods))
		for _, period := range periods {
			known[period.UserID] = true
		}
		for _, memberID := range memberIDs {
			if !known[memberID] {
				failure(c, http.StatusBadRequest, "INVALID_MEMBER", "选择了不属于工作区的成员", gin.H{"userId": memberID})
				return
			}
		}
	}
	var days []schedule.Day
	if len(memberIDs) > 0 {
		days, err = s.querySchedules(workspaceID, memberIDs, start, end)
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询团队排班", nil)
		return
	}
	periodByMember := make(map[uint64]memberPeriod, len(periods))
	for _, period := range periods {
		periodByMember[period.UserID] = period
	}
	summaries := make([]calendarservice.DaySummary, 0, len(dates))
	for _, date := range dates {
		effectiveMembers := make([]uint64, 0, len(memberIDs))
		for _, memberID := range memberIDs {
			if periodByMember[memberID].contains(date) {
				effectiveMembers = append(effectiveMembers, memberID)
			}
		}
		summaries = append(summaries, calendarservice.Aggregate([]schedule.Date{date}, effectiveMembers, days)[0])
	}
	response := make([]gin.H, 0, len(summaries))
	for _, summary := range summaries {
		members := make([]gin.H, 0, len(summary.Members))
		for _, member := range summary.Members {
			members = append(members, gin.H{"userId": member.UserID, "status": member.Status, "note": member.Note, "sourceType": member.SourceType,
				"sourceImportId": member.SourceImportID, "version": member.Version, "segments": segmentResponses(member.Segments)})
		}
		response = append(response, gin.H{
			"date": summary.Date.String(), "working": summary.Working, "rest": summary.Rest,
			"missing": summary.Missing, "allRest": summary.AllRest, "members": members,
		})
	}
	success(c, http.StatusOK, gin.H{"days": response, "memberIds": memberIDs})
}

type memberPeriod struct {
	UserID uint64
	Joined schedule.Date
	Left   *schedule.Date
}

func (p memberPeriod) contains(date schedule.Date) bool {
	return p.Joined.String() <= date.String() && (p.Left == nil || p.Left.String() >= date.String())
}

func (p memberPeriod) overlaps(start, end schedule.Date) bool {
	return p.Joined.String() <= end.String() && (p.Left == nil || p.Left.String() >= start.String())
}

func (s *server) memberPeriods(workspaceID uint64) ([]memberPeriod, error) {
	var timezone string
	if err := s.db.QueryRow(`SELECT timezone FROM workspaces WHERE id = ?`, workspaceID).Scan(&timezone); err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT user_id, joined_at, left_at FROM workspace_members WHERE workspace_id = ? ORDER BY user_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	periods := make([]memberPeriod, 0)
	for rows.Next() {
		var period memberPeriod
		var joined time.Time
		var left *time.Time
		if err := rows.Scan(&period.UserID, &joined, &left); err != nil {
			return nil, err
		}
		period.Joined = schedule.MustDate(joined.In(location).Format("2006-01-02"))
		if left != nil {
			leftDate := schedule.MustDate(left.In(location).Format("2006-01-02"))
			period.Left = &leftDate
		}
		periods = append(periods, period)
	}
	return periods, rows.Err()
}

func segmentResponses(segments []schedule.Segment) []gin.H {
	result := make([]gin.H, 0, len(segments))
	for _, segment := range segments {
		item := gin.H{"type": segment.Type, "crossDay": segment.CrossDay}
		if segment.ShiftID != nil {
			item["shiftId"] = *segment.ShiftID
			item["shiftName"] = segment.ShiftName
			item["shiftCode"] = segment.ShiftCode
			item["displayColor"] = segment.DisplayColor
		}
		if segment.StartTime != nil {
			item["startTime"] = segment.StartTime.String()
			item["endTime"] = segment.EndTime.String()
		}
		result = append(result, item)
	}
	return result
}

func parseMemberIDs(value string) ([]uint64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := map[uint64]bool{}
	result := make([]uint64, 0)
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil || id == 0 {
			return nil, errInvalidMemberID
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result, nil
}

var errInvalidMemberID = &memberIDError{}

type memberIDError struct{}

func (*memberIDError) Error() string { return "invalid member id" }

func dateRange(start, end schedule.Date, limit int) ([]schedule.Date, error) {
	startTime, _ := time.Parse("2006-01-02", start.String())
	endTime, _ := time.Parse("2006-01-02", end.String())
	if endTime.Before(startTime) {
		return nil, &dateRangeError{"结束日期不能早于开始日期"}
	}
	days := int(endTime.Sub(startTime).Hours()/24) + 1
	if days > limit {
		return nil, &dateRangeError{"日期范围不能超过 366 天"}
	}
	result := make([]schedule.Date, 0, days)
	for date := startTime; !date.After(endTime); date = date.AddDate(0, 0, 1) {
		result = append(result, schedule.MustDate(date.Format("2006-01-02")))
	}
	return result, nil
}

type dateRangeError struct{ message string }

func (e *dateRangeError) Error() string { return e.message }

func (s *server) activeMemberIDs(workspaceID uint64) ([]uint64, error) {
	rows, err := s.db.Query(`SELECT user_id FROM workspace_members WHERE workspace_id = ? AND status = 'ACTIVE' ORDER BY user_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]uint64, 0)
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
