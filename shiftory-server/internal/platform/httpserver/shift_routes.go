package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"

	"shiftory-server/internal/schedule"
)

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type shiftRequest struct {
	Name         string   `json:"name" binding:"required"`
	Code         string   `json:"code" binding:"required"`
	StartTime    *string  `json:"startTime"`
	EndTime      *string  `json:"endTime"`
	CrossDay     bool     `json:"crossDay"`
	DisplayColor string   `json:"displayColor" binding:"required"`
	Enabled      *bool    `json:"enabled"`
	SortOrder    int      `json:"sortOrder"`
	Aliases      []string `json:"aliases"`
}

func (s *server) createShift(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok {
		return
	}
	if !requireAdmin(member) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以管理班次", nil)
		return
	}
	var request shiftRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次信息不完整", nil)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Code = strings.ToUpper(strings.TrimSpace(request.Code))
	if request.Name == "" || request.Code == "" || !colorPattern.MatchString(request.DisplayColor) {
		failure(c, http.StatusBadRequest, "INVALID_SHIFT", "班次名称、代码或颜色无效", nil)
		return
	}
	if (request.StartTime == nil) != (request.EndTime == nil) {
		failure(c, http.StatusBadRequest, "INVALID_SHIFT_TIME", "开始和结束时间必须同时填写", nil)
		return
	}
	if request.StartTime != nil {
		start, startErr := schedule.ParseClock(*request.StartTime)
		end, endErr := schedule.ParseClock(*request.EndTime)
		if startErr != nil || endErr != nil {
			failure(c, http.StatusBadRequest, "INVALID_SHIFT_TIME", "班次时间格式无效", nil)
			return
		}
		if (!request.CrossDay && end <= start) || (request.CrossDay && end > start) {
			failure(c, http.StatusBadRequest, "INVALID_SHIFT_TIME", "班次跨日设置与时间不一致", nil)
			return
		}
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建班次", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var start, end any
	if request.StartTime != nil {
		start, end = *request.StartTime+":00", *request.EndTime+":00"
	}
	result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO shifts (workspace_id, name, code, start_time, end_time, cross_day, display_color, enabled, sort_order, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, request.Name, request.Code, start, end, request.CrossDay, request.DisplayColor, enabled, request.SortOrder, currentUserID(c))
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			failure(c, http.StatusConflict, "SHIFT_EXISTS", "班次代码已存在", nil)
			return
		}
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建班次", nil)
		return
	}
	shiftID, _ := result.LastInsertId()
	seen := map[string]bool{}
	for _, alias := range request.Aliases {
		alias = strings.TrimSpace(alias)
		normalized := strings.ToLower(alias)
		if alias == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO shift_aliases (workspace_id, shift_id, alias, alias_normalized) VALUES (?, ?, ?, ?)`, workspaceID, shiftID, alias, normalized); err != nil {
			var mysqlErr *mysql.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
				failure(c, http.StatusConflict, "SHIFT_ALIAS_EXISTS", "班次别名已被使用", gin.H{"alias": alias})
				return
			}
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法保存班次别名", nil)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交班次", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "SHIFT_CREATED", "shift", shiftID, gin.H{"name": request.Name, "code": request.Code})
	success(c, http.StatusCreated, gin.H{"id": uint64(shiftID), "name": request.Name, "code": request.Code, "startTime": request.StartTime, "endTime": request.EndTime, "crossDay": request.CrossDay, "displayColor": request.DisplayColor, "enabled": enabled, "sortOrder": request.SortOrder, "aliases": request.Aliases})
}

func (s *server) listShifts(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	if _, ok := s.requireWorkspaceMember(c, workspaceID); !ok {
		return
	}
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT id, name, code, start_time, end_time, cross_day, display_color, enabled, sort_order
FROM shifts WHERE workspace_id = ? ORDER BY sort_order, id`, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询班次", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id uint64
		var name, code, color string
		var start, end sql.NullString
		var cross, enabled bool
		var order int
		if err := rows.Scan(&id, &name, &code, &start, &end, &cross, &color, &enabled, &order); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取班次", nil)
			return
		}
		aliases, err := s.shiftAliases(id)
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取班次别名", nil)
			return
		}
		item := gin.H{"id": id, "name": name, "code": code, "crossDay": cross, "displayColor": color, "enabled": enabled, "sortOrder": order, "aliases": aliases}
		if start.Valid {
			item["startTime"] = strings.TrimSuffix(start.String, ":00")
			item["endTime"] = strings.TrimSuffix(end.String, ":00")
		}
		items = append(items, item)
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) shiftAliases(shiftID uint64) ([]string, error) {
	rows, err := s.db.Query(`SELECT alias FROM shift_aliases WHERE shift_id = ? ORDER BY id`, shiftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aliases := make([]string, 0)
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}
