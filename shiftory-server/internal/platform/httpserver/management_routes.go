package httpserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	calendarservice "shiftory-server/internal/calendar"
	"shiftory-server/internal/schedule"
)

var allowedThemes = map[string]bool{"mint": true, "sky": true, "lilac": true, "sakura": true, "amber": true, "graphite": true}

func (s *server) getPreferences(c *gin.Context) {
	var workspaceID sql.NullInt64
	var theme string
	err := s.db.QueryRowContext(c.Request.Context(), `SELECT current_workspace_id, theme FROM user_preferences WHERE user_id = ?`, currentUserID(c)).Scan(&workspaceID, &theme)
	if errors.Is(err, sql.ErrNoRows) {
		success(c, http.StatusOK, gin.H{"currentWorkspaceId": nil, "theme": "mint"})
		return
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询偏好", nil)
		return
	}
	result := gin.H{"currentWorkspaceId": nil, "theme": theme}
	if workspaceID.Valid {
		result["currentWorkspaceId"] = uint64(workspaceID.Int64)
	}
	success(c, http.StatusOK, result)
}

func (s *server) updatePreferences(c *gin.Context) {
	var request struct {
		CurrentWorkspaceID *uint64 `json:"currentWorkspaceId"`
		Theme              string  `json:"theme" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !allowedThemes[request.Theme] {
		failure(c, http.StatusBadRequest, "INVALID_PREFERENCE", "主题或工作区偏好无效", nil)
		return
	}
	if request.CurrentWorkspaceID != nil {
		if _, err := s.membership(*request.CurrentWorkspaceID, currentUserID(c)); err != nil {
			failure(c, http.StatusBadRequest, "INVALID_WORKSPACE", "当前工作区不可访问", nil)
			return
		}
	}
	_, err := s.db.ExecContext(c.Request.Context(), `
INSERT INTO user_preferences (user_id, current_workspace_id, theme) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE current_workspace_id = VALUES(current_workspace_id), theme = VALUES(theme)`, currentUserID(c), request.CurrentWorkspaceID, request.Theme)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存偏好", nil)
		return
	}
	success(c, http.StatusOK, gin.H{"currentWorkspaceId": request.CurrentWorkspaceID, "theme": request.Theme})
}

func (s *server) updateWorkspace(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(member) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以修改工作区", nil)
		}
		return
	}
	var request createWorkspaceRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		failure(c, http.StatusBadRequest, "INVALID_WORKSPACE", "工作区信息无效", nil)
		return
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_TIMEZONE", "工作区时区无效", nil)
		return
	}
	if _, err := s.db.ExecContext(c.Request.Context(), `UPDATE workspaces SET name = ?, timezone = ? WHERE id = ?`, strings.TrimSpace(request.Name), request.Timezone, workspaceID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新工作区", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "WORKSPACE_UPDATED", "workspace", workspaceID, gin.H{"name": request.Name, "timezone": request.Timezone})
	success(c, http.StatusOK, gin.H{"id": workspaceID, "name": strings.TrimSpace(request.Name), "timezone": request.Timezone})
}

func (s *server) updateMember(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	targetID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	actor, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(actor) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以管理成员", nil)
		}
		return
	}
	var targetRole string
	if err := s.db.QueryRow(`SELECT role FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspaceID, targetID).Scan(&targetRole); err != nil {
		failure(c, http.StatusNotFound, "MEMBER_NOT_FOUND", "成员不存在", nil)
		return
	}
	if targetRole == "OWNER" || (actor.Role == "ADMIN" && targetRole == "ADMIN") {
		failure(c, http.StatusForbidden, "FORBIDDEN", "不能修改该成员", nil)
		return
	}
	var request struct {
		Role   string `json:"role" binding:"required"`
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || (request.Role != "ADMIN" && request.Role != "MEMBER") || (request.Status != "ACTIVE" && request.Status != "DISABLED" && request.Status != "REMOVED") {
		failure(c, http.StatusBadRequest, "INVALID_MEMBER", "成员角色或状态无效", nil)
		return
	}
	if actor.Role == "ADMIN" && request.Role == "ADMIN" {
		failure(c, http.StatusForbidden, "FORBIDDEN", "只有所有者可以任免管理员", nil)
		return
	}
	var left any
	if request.Status == "REMOVED" {
		left = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(c.Request.Context(), `
UPDATE workspace_members SET role = ?, status = ?, left_at = ? WHERE workspace_id = ? AND user_id = ?`, request.Role, request.Status, left, workspaceID, targetID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新成员", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "MEMBER_UPDATED", "user", targetID, request)
	success(c, http.StatusOK, gin.H{"userId": targetID, "role": request.Role, "status": request.Status})
}

func (s *server) updateShift(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	shiftID, ok := parseID(c, "shiftId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(member) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以管理班次", nil)
		}
		return
	}
	var request shiftRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil || !colorPattern.MatchString(request.DisplayColor) {
		failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次信息无效", nil)
		return
	}
	request.Name, request.Code = strings.TrimSpace(request.Name), strings.ToUpper(strings.TrimSpace(request.Code))
	if request.Name == "" || request.Code == "" || (request.StartTime == nil) != (request.EndTime == nil) {
		failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次信息无效", nil)
		return
	}
	var start, end any
	if request.StartTime != nil {
		startClock, startErr := schedule.ParseClock(*request.StartTime)
		endClock, endErr := schedule.ParseClock(*request.EndTime)
		if startErr != nil || endErr != nil || (!request.CrossDay && endClock <= startClock) || (request.CrossDay && endClock > startClock) {
			failure(c, http.StatusBadRequest, "INVALID_SHIFT_TIME", "班次时间或跨日设置无效", nil)
			return
		}
		start, end = *request.StartTime+":00", *request.EndTime+":00"
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新班次", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(c.Request.Context(), `
UPDATE shifts SET name = ?, code = ?, start_time = ?, end_time = ?, cross_day = ?, display_color = ?, enabled = ?, sort_order = ?
WHERE id = ? AND workspace_id = ?`, request.Name, request.Code, start, end, request.CrossDay, request.DisplayColor, *request.Enabled, request.SortOrder, shiftID, workspaceID)
	if err != nil {
		failure(c, http.StatusConflict, "SHIFT_CONFLICT", "班次代码冲突", nil)
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		failure(c, http.StatusNotFound, "SHIFT_NOT_FOUND", "班次不存在", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM shift_aliases WHERE shift_id = ?`, shiftID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新班次别名", nil)
		return
	}
	seen := map[string]bool{}
	for _, alias := range request.Aliases {
		alias = strings.TrimSpace(alias)
		normalized := strings.ToLower(alias)
		if alias == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		if _, err := tx.ExecContext(c.Request.Context(), `INSERT INTO shift_aliases (workspace_id, shift_id, alias, alias_normalized) VALUES (?, ?, ?, ?)`, workspaceID, shiftID, alias, normalized); err != nil {
			failure(c, http.StatusConflict, "SHIFT_ALIAS_EXISTS", "班次别名已被使用", gin.H{"alias": alias})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交班次", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "SHIFT_UPDATED", "shift", shiftID, request)
	success(c, http.StatusOK, gin.H{"id": shiftID, "name": request.Name, "code": request.Code, "enabled": *request.Enabled})
}

func (s *server) batchSchedules(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	actor, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return
	}
	var request struct {
		UserID   uint64                   `json:"userId" binding:"required"`
		Dates    []string                 `json:"dates" binding:"required"`
		Status   schedule.Status          `json:"status" binding:"required"`
		Note     string                   `json:"note"`
		Segments []scheduleSegmentRequest `json:"segments"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Dates) == 0 || len(request.Dates) > 366 {
		failure(c, http.StatusBadRequest, "INVALID_BATCH", "批量排班信息无效", nil)
		return
	}
	if actor.UserID != request.UserID && !requireAdmin(actor) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "不能修改其他成员的排班", nil)
		return
	}
	if !s.userBelongsToWorkspace(workspaceID, request.UserID) {
		failure(c, http.StatusBadRequest, "INVALID_MEMBER", "目标用户不属于当前工作区", nil)
		return
	}
	type pendingDay struct{ day schedule.Day }
	pending := make([]pendingDay, 0, len(request.Dates))
	seen := map[string]bool{}
	for _, rawDate := range request.Dates {
		date, err := schedule.ParseDate(rawDate)
		if err != nil || seen[rawDate] {
			failure(c, http.StatusBadRequest, "INVALID_DATE", "批量排班日期无效", gin.H{"date": rawDate})
			return
		}
		seen[rawDate] = true
		segments, err := s.prepareSegments(workspaceID, request.Segments)
		if err != nil {
			failure(c, http.StatusBadRequest, "INVALID_SEGMENT", err.Error(), nil)
			return
		}
		day := schedule.Day{WorkspaceID: workspaceID, UserID: request.UserID, WorkDate: date, Status: request.Status, SourceType: schedule.SourceManual, Note: request.Note, CreatedBy: currentUserID(c), Segments: segments}
		if err := day.Validate(); err != nil {
			failure(c, http.StatusBadRequest, "INVALID_SCHEDULE", err.Error(), nil)
			return
		}
		pending = append(pending, pendingDay{day: day})
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法开始批量排班事务", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	items := make([]gin.H, 0, len(pending))
	for _, item := range pending {
		existing, found, err := queryScheduleTx(c.Request.Context(), tx, workspaceID, request.UserID, item.day.WorkDate, true)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法锁定批量排班", gin.H{"date": item.day.WorkDate.String()})
			return
		}
		saved, err := writeScheduleTx(c.Request.Context(), tx, item.day, existing, found)
		if err != nil {
			failure(c, http.StatusConflict, "BATCH_CONFLICT", "批量排班发生冲突", gin.H{"date": item.day.WorkDate.String()})
			return
		}
		var beforeVersion, beforeJSON any
		changeType := "BATCH_CREATE"
		if found {
			beforeVersion = existing.Version
			beforeJSON, _ = json.Marshal(storedFromDay(existing))
			changeType = "BATCH_UPDATE"
		}
		afterJSON, _ := json.Marshal(storedFromDay(saved))
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, changed_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, saved.ID, workspaceID, request.UserID, saved.WorkDate.String(), beforeVersion, saved.Version, beforeJSON, afterJSON, changeType, currentUserID(c)); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法记录批量排班历史", nil)
			return
		}
		items = append(items, scheduleResponse(saved))
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交批量排班", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "SCHEDULE_BATCH_UPDATED", "user", request.UserID, gin.H{"dates": request.Dates})
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) prepareSegments(workspaceID uint64, input []scheduleSegmentRequest) ([]schedule.Segment, error) {
	segments := make([]schedule.Segment, 0, len(input))
	for index, item := range input {
		segment := schedule.Segment{Type: item.Type, ShiftID: item.ShiftID, CrossDay: item.CrossDay, SortOrder: index}
		if item.Type == schedule.SegmentShift && item.ShiftID != nil {
			if err := s.loadShiftSnapshot(workspaceID, *item.ShiftID, &segment); err != nil {
				return nil, err
			}
		} else if item.Type == schedule.SegmentTimeRange && item.StartTime != nil && item.EndTime != nil {
			start, startErr := schedule.ParseClock(*item.StartTime)
			end, endErr := schedule.ParseClock(*item.EndTime)
			if startErr != nil || endErr != nil {
				return nil, schedule.ErrInvalidClock
			}
			segment.StartTime, segment.EndTime = &start, &end
		} else {
			return nil, schedule.ErrInvalidSegmentType
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func (s *server) scheduleHistory(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	userID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	date, err := schedule.ParseDate(c.Param("date"))
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_DATE", "日期无效", nil)
		return
	}
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT id, before_version, after_version, before_snapshot, after_snapshot, change_type, changed_by, created_at
FROM schedule_revisions WHERE workspace_id = ? AND user_id = ? AND work_date = ? ORDER BY created_at DESC`, workspaceID, userID, date.String())
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询排班历史", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, actor uint64
		var beforeVersion, afterVersion sql.NullInt64
		var before, after []byte
		var changeType string
		var createdAt time.Time
		if err := rows.Scan(&id, &beforeVersion, &afterVersion, &before, &after, &changeType, &actor, &createdAt); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取排班历史", nil)
			return
		}
		item := gin.H{"id": id, "changeType": changeType, "changedBy": actor, "createdAt": createdAt}
		if len(before) > 0 {
			var value any
			_ = json.Unmarshal(before, &value)
			item["before"] = value
		}
		if len(after) > 0 {
			var value any
			_ = json.Unmarshal(after, &value)
			item["after"] = value
		}
		items = append(items, item)
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) overview(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	var timezone string
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT timezone FROM workspaces WHERE id = ?`, workspaceID).Scan(&timezone); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取工作区时区", nil)
		return
	}
	location, _ := time.LoadLocation(timezone)
	date, err := schedule.ParseDate(c.Query("date"))
	if err != nil {
		date = schedule.MustDate(time.Now().In(location).Format("2006-01-02"))
	}
	memberIDs, err := s.activeMemberIDs(workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询概览", nil)
		return
	}
	days, err := s.querySchedules(workspaceID, memberIDs, date, date)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询概览", nil)
		return
	}
	summary := calendarservice.Aggregate([]schedule.Date{date}, memberIDs, days)[0]
	var pending int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM import_jobs WHERE workspace_id = ? AND upload_user_id = ? AND state IN ('PENDING', 'PARSING', 'NEEDS_REVIEW')`, workspaceID, currentUserID(c)).Scan(&pending)
	parsedDate, _ := time.Parse("2006-01-02", date.String())
	monthStart := parsedDate.AddDate(0, 0, 1-parsedDate.Day())
	monthEnd := monthStart.AddDate(0, 1, -1)
	var scheduledThisMonth int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM schedule_days WHERE user_id = ? AND work_date BETWEEN ? AND ?`, workspaceID, currentUserID(c), monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02")).Scan(&scheduledThisMonth)
	completeness := float64(scheduledThisMonth) / float64(monthEnd.Day()) * 100
	var nextWorking, nextRest sql.NullString
	_ = s.db.QueryRow(`SELECT DATE_FORMAT(work_date, '%Y-%m-%d') FROM schedule_days WHERE user_id = ? AND work_date >= ? AND status = 'WORKING' ORDER BY work_date LIMIT 1`, workspaceID, currentUserID(c), date.String()).Scan(&nextWorking)
	_ = s.db.QueryRow(`SELECT DATE_FORMAT(work_date, '%Y-%m-%d') FROM schedule_days WHERE user_id = ? AND work_date >= ? AND status = 'REST' ORDER BY work_date LIMIT 1`, workspaceID, currentUserID(c), date.String()).Scan(&nextRest)
	windowEnd := schedule.MustDate(parsedDate.AddDate(0, 1, 0).Format("2006-01-02"))
	windowDays, _ := dateRange(date, windowEnd, 366)
	windowSchedules, _ := s.querySchedules(workspaceID, memberIDs, date, windowEnd)
	windowSummaries := calendarservice.Aggregate(windowDays, memberIDs, windowSchedules)
	allRestDates := make([]string, 0, 5)
	for _, day := range windowSummaries {
		if day.AllRest {
			allRestDates = append(allRestDates, day.Date.String())
			if len(allRestDates) == 5 {
				break
			}
		}
	}
	result := gin.H{"date": date.String(), "working": summary.Working, "rest": summary.Rest, "missing": summary.Missing, "allRest": summary.AllRest,
		"pendingImports": pending, "monthCompleteness": completeness, "allRestDates": allRestDates, "nextWorkingDate": nil, "nextRestDate": nil}
	if nextWorking.Valid {
		result["nextWorkingDate"] = nextWorking.String
	}
	if nextRest.Valid {
		result["nextRestDate"] = nextRest.String
	}
	success(c, http.StatusOK, result)
}

func (s *server) listAudits(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(member) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以查看审计日志", nil)
		}
		return
	}
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT id, actor_user_id, action, target_type, target_id, details, created_at
FROM audit_logs WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 200`, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询审计日志", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id uint64
		var actor sql.NullInt64
		var action, targetType string
		var targetID sql.NullString
		var details []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &actor, &action, &targetType, &targetID, &details, &createdAt); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取审计日志", nil)
			return
		}
		var detailValue any
		_ = json.Unmarshal(details, &detailValue)
		items = append(items, gin.H{"id": id, "actorUserId": actor.Int64, "action": action, "targetType": targetType, "targetId": targetID.String, "details": detailValue, "createdAt": createdAt})
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) recordAudit(c *gin.Context, workspaceID, actorID uint64, action, targetType string, targetID any, details any) {
	payload, _ := json.Marshal(details)
	requestID, _ := c.Get(requestIDKey)
	userAgent := c.Request.UserAgent()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, _ = s.db.ExecContext(c.Request.Context(), `
INSERT INTO audit_logs (workspace_id, actor_user_id, action, target_type, target_id, request_id, ip_address, user_agent, details)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, actorID, action, targetType, targetID, requestID, c.ClientIP(), userAgent, payload)
}
