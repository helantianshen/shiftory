package httpserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"shiftory-server/internal/schedule"
)

// scheduleSegmentRequest 承载班次引用或自定义时间，展示快照由服务端补全
type scheduleSegmentRequest struct {
	ExistingSegmentID *uint64              `json:"existingSegmentId"`
	Type              schedule.SegmentType `json:"type" binding:"required"`
	ShiftID           *uint64              `json:"shiftId"`
	StartTime         *string              `json:"startTime"`
	EndTime           *string              `json:"endTime"`
	CrossDay          bool                 `json:"crossDay"`
}

// scheduleRequest 承载日排班内容和客户端已知版本
type scheduleRequest struct {
	Status   schedule.Status          `json:"status" binding:"required"`
	Note     string                   `json:"note"`
	Version  uint64                   `json:"version"`
	Segments []scheduleSegmentRequest `json:"segments"`
}

// upsertSchedule 检查编辑权限、构造领域排班并按客户端版本保存
func (s *server) upsertSchedule(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	targetUserID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	actor, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return
	}
	if actor.UserID != targetUserID && !requireAdmin(actor) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "不能修改其他成员的排班", nil)
		return
	}
	if !s.userBelongsToWorkspace(workspaceID, targetUserID) {
		failure(c, http.StatusBadRequest, "INVALID_MEMBER", "目标用户不属于当前工作区", nil)
		return
	}
	date, err := schedule.ParseDate(c.Param("date"))
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_DATE", "排班日期无效", nil)
		return
	}
	var request scheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_SCHEDULE", "排班信息不完整", nil)
		return
	}
	// 复用历史班次必须绑定当前用户和日期，客户端不能自行提交展示快照
	existingSegments := map[uint64]schedule.Segment{}
	for _, item := range request.Segments {
		if item.ExistingSegmentID == nil {
			continue
		}
		days, err := s.querySchedules(workspaceID, []uint64{targetUserID}, date, date)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取历史班次", nil)
			return
		}
		for _, day := range days {
			for _, segment := range day.Segments {
				existingSegments[segment.ID] = segment
			}
		}
		break
	}
	// 把请求时间段转换为领域对象，引用班次使用服务端快照而非客户端展示值
	segments := make([]schedule.Segment, 0, len(request.Segments))
	for index, item := range request.Segments {
		segment := schedule.Segment{Type: item.Type, ShiftID: item.ShiftID, CrossDay: item.CrossDay, SortOrder: index}
		if item.ExistingSegmentID != nil {
			preserved, exists := existingSegments[*item.ExistingSegmentID]
			if !exists || item.Type != preserved.Type {
				failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "历史时间段不属于该日期", nil)
				return
			}
			preserved.SortOrder = index
			segments = append(segments, preserved)
			continue
		}
		switch item.Type {
		case schedule.SegmentShift:
			if item.ShiftID == nil {
				failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "班次时间段缺少班次 ID", nil)
				return
			}
			if err := s.loadShiftSnapshot(workspaceID, *item.ShiftID, &segment); err != nil {
				failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次不存在或已禁用", nil)
				return
			}
		case schedule.SegmentTimeRange:
			if item.StartTime != nil {
				clock, err := schedule.ParseClock(*item.StartTime)
				if err != nil {
					failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "开始时间无效", nil)
					return
				}
				segment.StartTime = &clock
			}
			if item.EndTime != nil {
				clock, err := schedule.ParseClock(*item.EndTime)
				if err != nil {
					failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "结束时间无效", nil)
					return
				}
				segment.EndTime = &clock
			}
		default:
			failure(c, http.StatusBadRequest, "INVALID_SEGMENT", "时间段类型无效", nil)
			return
		}
		segments = append(segments, segment)
	}
	day := schedule.Day{WorkspaceID: workspaceID, UserID: targetUserID, WorkDate: date, Status: request.Status, SourceType: schedule.SourceManual, Note: strings.TrimSpace(request.Note), Version: request.Version, CreatedBy: currentUserID(c), Segments: segments}
	if err := day.Validate(); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_SCHEDULE", err.Error(), nil)
		return
	}
	saved, err := s.saveSchedule(c, day)
	if errors.Is(err, errVersionConflict) {
		failure(c, http.StatusConflict, "VERSION_CONFLICT", "排班已被其他操作修改", nil)
		return
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存排班", gin.H{"reason": err.Error()})
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "SCHEDULE_UPSERTED", "schedule_day", saved.ID, gin.H{"userId": targetUserID, "date": date.String(), "version": saved.Version})
	success(c, http.StatusOK, scheduleResponse(saved))
}

var errVersionConflict = errors.New("schedule version conflict")

// saveSchedule 锁定当前排班并校验版本，在事务中保存主体、时间段和修订
func (s *server) saveSchedule(c *gin.Context, day schedule.Day) (schedule.Day, error) {
	// 排班主体、时间段和版本快照必须在同一事务中写入
	// 客户端版本与锁定版本不一致时整次操作按并发冲突拒绝
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		return schedule.Day{}, err
	}
	defer func() { _ = tx.Rollback() }()
	existing, found, err := queryScheduleTx(c.Request.Context(), tx, day.WorkspaceID, day.UserID, day.WorkDate, true)
	creating := !found
	if err != nil {
		return schedule.Day{}, err
	}
	// 创建要求客户端版本为零，更新要求与锁定版本一致并递增版本
	if creating {
		if day.Version != 0 {
			return schedule.Day{}, errVersionConflict
		}
		result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_days (workspace_id, user_id, work_date, status, source_type, note, version, created_by)
VALUES (?, ?, ?, ?, ?, ?, 1, ?)`, day.WorkspaceID, day.UserID, day.WorkDate.String(), day.Status, day.SourceType, day.Note, day.CreatedBy)
		if err != nil {
			return schedule.Day{}, err
		}
		id, _ := result.LastInsertId()
		day.ID, day.Version = uint64(id), 1
	} else {
		if day.Version == 0 || day.Version != existing.Version {
			return schedule.Day{}, errVersionConflict
		}
		day.ID, day.Version = existing.ID, existing.Version+1
		if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE schedule_days SET status = ?, source_type = ?, source_import_id = NULL, note = ?, version = ?
WHERE id = ? AND version = ?`, day.Status, day.SourceType, day.Note, day.Version, day.ID, existing.Version); err != nil {
			return schedule.Day{}, err
		}
		if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM schedule_segments WHERE schedule_day_id = ?`, day.ID); err != nil {
			return schedule.Day{}, err
		}
	}
	// 时间段整体重写，保存与本次排班一致的班次名称、颜色和时间快照
	for index, segment := range day.Segments {
		var start, end any
		if segment.StartTime != nil {
			start, end = segment.StartTime.String()+":00", segment.EndTime.String()+":00"
		}
		result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_segments
    (schedule_day_id, segment_type, shift_id, shift_name_snapshot, shift_code_snapshot, start_time, end_time, cross_day, display_color_snapshot, sort_order, original_label)
VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''))`,
			day.ID, segment.Type, segment.ShiftID, segment.ShiftName, segment.ShiftCode, start, end,
			segment.CrossDay, segment.DisplayColor, segment.SortOrder, segment.OriginalLabel)
		if err != nil {
			return schedule.Day{}, err
		}
		segmentID, err := result.LastInsertId()
		if err != nil {
			return schedule.Day{}, err
		}
		day.Segments[index].ID = uint64(segmentID)
	}
	// 记录前后业务快照与版本，历史查询和安全撤销不依赖通用审计文本
	after, _ := json.Marshal(storedFromDay(day))
	changeType := "CREATE"
	var beforeVersion, beforeSnapshot any
	if !creating {
		changeType, beforeVersion = "UPDATE", existing.Version
		before, _ := json.Marshal(storedFromDay(existing))
		beforeSnapshot = before
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO schedule_revisions
    (schedule_day_id, workspace_id, user_id, work_date, before_version, after_version, before_snapshot, after_snapshot, change_type, changed_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, day.ID, day.WorkspaceID, day.UserID, day.WorkDate.String(), beforeVersion, day.Version, beforeSnapshot, after, changeType, day.CreatedBy); err != nil {
		return schedule.Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return schedule.Day{}, err
	}
	return day, nil
}

// loadShiftSnapshot 从工作区启用班次读取展示与时间信息并填入时间段
func (s *server) loadShiftSnapshot(workspaceID, shiftID uint64, segment *schedule.Segment) error {
	var start, end sql.NullString
	err := s.db.QueryRow(`
SELECT name, code, TIME_FORMAT(start_time, '%H:%i'), TIME_FORMAT(end_time, '%H:%i'), cross_day, display_color
FROM shifts WHERE id = ? AND workspace_id = ? AND enabled = TRUE`, shiftID, workspaceID).Scan(
		&segment.ShiftName, &segment.ShiftCode, &start, &end, &segment.CrossDay, &segment.DisplayColor)
	if err != nil {
		return err
	}
	if start.Valid {
		startClock, _ := schedule.ParseClock(start.String)
		endClock, _ := schedule.ParseClock(end.String)
		segment.StartTime, segment.EndTime = &startClock, &endClock
	}
	return nil
}

// userBelongsToWorkspace 检查目标用户是否为有效成员，查询失败时视为不属于工作区
func (s *server) userBelongsToWorkspace(workspaceID, userID uint64) bool {
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND user_id = ? AND status = 'ACTIVE'`, workspaceID, userID).Scan(&count)
	return count == 1
}

// listSchedules 在当前工作区鉴权后查询目标用户指定日期范围的排班
func (s *server) listSchedules(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	targetUserID, ok := parseID(c, "userId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	period, ok := s.requireScheduleReader(c, workspaceID, targetUserID)
	if !ok {
		return
	}
	start, startErr := schedule.ParseDate(c.Query("start"))
	end, endErr := schedule.ParseDate(c.Query("end"))
	if startErr != nil || endErr != nil || start.String() > end.String() {
		failure(c, http.StatusBadRequest, "INVALID_RANGE", "日期范围无效", nil)
		return
	}
	days, err := s.querySchedules(workspaceID, []uint64{targetUserID}, start, end)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询排班", nil)
		return
	}
	items := make([]gin.H, 0, len(days))
	for _, day := range days {
		if period.contains(day.WorkDate) {
			items = append(items, scheduleResponse(day))
		}
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

// querySchedules 按用户与日期查询全局排班，workspaceID 仅填入返回上下文
func (s *server) querySchedules(workspaceID uint64, userIDs []uint64, start, end schedule.Date) ([]schedule.Day, error) {
	if len(userIDs) == 0 {
		return []schedule.Day{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")
	args := make([]any, 0, len(userIDs)+2)
	args = append(args, start.String(), end.String())
	for _, id := range userIDs {
		args = append(args, id)
	}
	rows, err := s.db.Query(`
SELECT id, user_id, DATE_FORMAT(work_date, '%Y-%m-%d'), status, source_type, source_import_id, note, version, created_by
FROM schedule_days
WHERE work_date BETWEEN ? AND ? AND user_id IN (`+placeholders+`)
ORDER BY work_date, user_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := make([]schedule.Day, 0)
	for rows.Next() {
		var day schedule.Day
		var date string
		var importID sql.NullInt64
		if err := rows.Scan(&day.ID, &day.UserID, &date, &day.Status, &day.SourceType, &importID, &day.Note, &day.Version, &day.CreatedBy); err != nil {
			return nil, err
		}
		day.WorkspaceID, day.WorkDate = workspaceID, schedule.MustDate(date)
		if importID.Valid {
			value := uint64(importID.Int64)
			day.SourceImportID = &value
		}
		segments, err := s.querySegments(day.ID)
		if err != nil {
			return nil, err
		}
		day.Segments = segments
		days = append(days, day)
	}
	return days, rows.Err()
}

// querySegments 按展示顺序读取排班时间段及保存的班次快照
func (s *server) querySegments(dayID uint64) ([]schedule.Segment, error) {
	rows, err := s.db.Query(`
SELECT id, segment_type, shift_id, COALESCE(shift_name_snapshot, ''), COALESCE(shift_code_snapshot, ''),
       TIME_FORMAT(start_time, '%H:%i'), TIME_FORMAT(end_time, '%H:%i'), cross_day,
       COALESCE(display_color_snapshot, ''), sort_order, COALESCE(original_label, '')
FROM schedule_segments WHERE schedule_day_id = ? ORDER BY sort_order, id`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	segments := make([]schedule.Segment, 0)
	for rows.Next() {
		var segment schedule.Segment
		var shiftID sql.NullInt64
		var start, end sql.NullString
		if err := rows.Scan(&segment.ID, &segment.Type, &shiftID, &segment.ShiftName, &segment.ShiftCode, &start, &end,
			&segment.CrossDay, &segment.DisplayColor, &segment.SortOrder, &segment.OriginalLabel); err != nil {
			return nil, err
		}
		if shiftID.Valid {
			value := uint64(shiftID.Int64)
			segment.ShiftID = &value
		}
		if start.Valid {
			startClock, _ := schedule.ParseClock(start.String)
			endClock, _ := schedule.ParseClock(end.String)
			segment.StartTime, segment.EndTime = &startClock, &endClock
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

// scheduleResponse 按排序字段组织时间段并生成排班接口响应，不修改输入切片
func scheduleResponse(day schedule.Day) gin.H {
	segments := make([]gin.H, 0, len(day.Segments))
	ordered := append([]schedule.Segment(nil), day.Segments...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SortOrder < ordered[j].SortOrder })
	for _, segment := range ordered {
		item := gin.H{"id": segment.ID, "type": segment.Type, "crossDay": segment.CrossDay, "sortOrder": segment.SortOrder, "originalLabel": segment.OriginalLabel}
		if segment.ShiftID != nil {
			item["shiftId"] = *segment.ShiftID
		}
		if segment.ShiftName != "" {
			item["shiftName"] = segment.ShiftName
			item["shiftCode"] = segment.ShiftCode
			item["displayColor"] = segment.DisplayColor
		}
		if segment.StartTime != nil {
			item["startTime"] = segment.StartTime.String()
			item["endTime"] = segment.EndTime.String()
		}
		segments = append(segments, item)
	}
	return gin.H{"id": day.ID, "workspaceId": day.WorkspaceID, "userId": day.UserID, "workDate": day.WorkDate.String(), "status": day.Status, "sourceType": day.SourceType, "sourceImportId": day.SourceImportID, "note": day.Note, "version": day.Version, "segments": segments}
}

// requireScheduleReader 限制目标为工作区现有或历史成员，并返回日历使用的日期可见范围
func (s *server) requireScheduleReader(c *gin.Context, workspaceID, userID uint64) (memberPeriod, bool) {
	periods, err := s.memberPeriods(workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法检查成员范围", nil)
		return memberPeriod{}, false
	}
	for _, period := range periods {
		if period.UserID == userID {
			return period, true
		}
	}
	failure(c, http.StatusForbidden, "FORBIDDEN", "无权查看该用户的排班", nil)
	return memberPeriod{}, false
}
