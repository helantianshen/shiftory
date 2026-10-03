package httpserver

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// membership 表示一次权限检查读取到的工作区成员身份
type membership struct {
	UserID uint64
	Role   string
	Status string
}

// membership 查询有效工作区成员关系，失效成员不能据此取得访问权限
func (s *server) membership(workspaceID, userID uint64) (membership, error) {
	var member membership
	err := s.db.QueryRow(`
SELECT user_id, role, status FROM workspace_members
WHERE workspace_id = $1 AND user_id = $2 AND status = 'ACTIVE'`, workspaceID, userID).Scan(&member.UserID, &member.Role, &member.Status)
	return member, err
}

// requireAdmin 判断已加载的成员角色是否为所有者或管理员
func requireAdmin(member membership) bool { return member.Role == "OWNER" || member.Role == "ADMIN" }

// parseID 读取非零路径 ID，失败时写入错误响应并返回 false
func parseID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		failure(c, http.StatusBadRequest, "INVALID_ID", "资源 ID 无效", nil)
		return 0, false
	}
	return id, true
}

// createWorkspaceRequest 承载工作区名称及 IANA 时区
type createWorkspaceRequest struct {
	Name     string `json:"name" binding:"required"`
	Timezone string `json:"timezone" binding:"required"`
}

// createWorkspace 校验名称与时区，在事务中创建工作区、所有者关系和预置班次
func (s *server) createWorkspace(c *gin.Context) {
	var request createWorkspaceRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		failure(c, http.StatusBadRequest, "INVALID_WORKSPACE", "工作区信息不完整", nil)
		return
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_TIMEZONE", "工作区时区无效", nil)
		return
	}
	userID := currentUserID(c)
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建工作区", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	err = tx.QueryRowContext(c.Request.Context(), `
INSERT INTO workspaces (name, timezone, owner_user_id, created_by) VALUES ($1, $2, $3, $4) RETURNING id`, strings.TrimSpace(request.Name), request.Timezone, userID, userID).Scan(&id)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建工作区", nil)
		return
	}

	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO workspace_members (workspace_id, user_id, role, status) VALUES ($1, $2, 'OWNER', 'ACTIVE')`, id, userID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建所有者关系", nil)
		return
	}
	// 预置班次与所有者成员关系共用创建事务，避免出现只有部分初始数据的工作区
	defaults := []struct {
		name, code, start, end, color string
		cross                         bool
	}{
		{"早班", "MORNING", "08:00:00", "16:00:00", "#22a06b", false},
		{"中班", "MIDDLE", "16:00:00", "00:00:00", "#3b82f6", true},
		{"晚班", "NIGHT", "00:00:00", "08:00:00", "#8b5cf6", false},
	}
	for index, shift := range defaults {
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO shifts (workspace_id, name, code, start_time, end_time, cross_day, display_color, sort_order, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, shift.name, shift.code, shift.start, shift.end, shift.cross, shift.color, index, userID); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建默认班次", nil)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交工作区", nil)
		return
	}
	s.recordAudit(c, uint64(id), userID, "WORKSPACE_CREATED", "workspace", id, gin.H{"timezone": request.Timezone})
	success(c, http.StatusCreated, gin.H{"id": uint64(id), "name": strings.TrimSpace(request.Name), "timezone": request.Timezone, "role": "OWNER"})
}

// listWorkspaces 列出当前用户仍具有有效成员关系的工作区
func (s *server) listWorkspaces(c *gin.Context) {
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT w.id, w.name, w.timezone, m.role
FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = $1 AND m.status = 'ACTIVE' ORDER BY w.created_at`, currentUserID(c))
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询工作区", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id uint64
		var name, timezone, role string
		if err := rows.Scan(&id, &name, &timezone, &role); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取工作区", nil)
			return
		}
		items = append(items, gin.H{"id": id, "name": name, "timezone": timezone, "role": role})
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

// listMembers 查询工作区成员资料及排班完整度、最近导入等摘要
func (s *server) listMembers(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	if _, err := s.membership(workspaceID, currentUserID(c)); err != nil {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权访问工作区", nil)
		return
	}
	var timezone string
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT timezone FROM workspaces WHERE id = $1`, workspaceID).Scan(&timezone); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取工作区时区", nil)
		return
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		failure(c, http.StatusInternalServerError, "INVALID_TIMEZONE", "工作区时区配置无效", nil)
		return
	}
	now := time.Now().In(location)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	monthEnd := monthStart.AddDate(0, 1, -1)
	daysInMonth := monthEnd.Day()
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT u.id, u.username, u.email, u.display_name, COALESCE(u.avatar_url, ''), m.role, m.status, m.joined_at, m.left_at,
       (SELECT COUNT(*) FROM schedule_days sd
        WHERE sd.user_id = m.user_id
          AND sd.work_date BETWEEN $1 AND $2),
       (SELECT MAX(ij.created_at) FROM import_jobs ij
        WHERE ij.workspace_id = m.workspace_id AND ij.target_user_id = m.user_id
          AND ij.state IN ('COMPLETED', 'ROLLED_BACK'))
FROM workspace_members m JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = $3 ORDER BY CASE m.role WHEN 'OWNER' THEN 1 WHEN 'ADMIN' THEN 2 WHEN 'MEMBER' THEN 3 ELSE 0 END, u.display_name`, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"), workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询成员", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id uint64
		var username, email, displayName, avatar, role, status string
		var joined time.Time
		var left, lastImport sql.NullTime
		var scheduledDays int
		if err := rows.Scan(&id, &username, &email, &displayName, &avatar, &role, &status, &joined, &left, &scheduledDays, &lastImport); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取成员", nil)
			return
		}
		completeness := float64(scheduledDays*1000/daysInMonth) / 10
		item := gin.H{"id": id, "username": username, "email": email, "displayName": displayName, "avatarUrl": avatar, "role": role, "status": status, "joinedAt": joined, "scheduleCompleteness": completeness}
		if left.Valid {
			item["leftAt"] = left.Time
		}
		if lastImport.Valid {
			item["lastImportAt"] = lastImport.Time
		}
		items = append(items, item)
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

// invitationRequest 承载邮箱或用户名二选一的邀请目标及角色
type invitationRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role" binding:"required"`
}

// createInvitation 校验邀请方式与管理员权限，保存令牌摘要并返回邀请信息
func (s *server) createInvitation(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, err := s.membership(workspaceID, currentUserID(c))
	if err != nil || !requireAdmin(member) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以邀请成员", nil)
		return
	}
	var request invitationRequest
	if err := c.ShouldBindJSON(&request); err != nil || (request.Role != "MEMBER" && request.Role != "ADMIN") || (strings.TrimSpace(request.Email) == "" && strings.TrimSpace(request.Username) == "") || (strings.TrimSpace(request.Email) != "" && strings.TrimSpace(request.Username) != "") {
		failure(c, http.StatusBadRequest, "INVALID_INVITATION", "邀请信息无效", nil)
		return
	}
	if request.Role == "ADMIN" && member.Role != "OWNER" {
		failure(c, http.StatusForbidden, "FORBIDDEN", "只有所有者可以邀请管理员", nil)
		return
	}
	// 用户名邀请先解析有效账号的邮箱，两种入口最终沿用同一邮箱归属校验
	var inviteEmail, inviteUsername string
	if strings.TrimSpace(request.Username) != "" {
		inviteUsername = strings.TrimSpace(request.Username)
		err = s.db.QueryRowContext(c.Request.Context(), `SELECT username, email_normalized FROM users WHERE username_normalized = LOWER($1) AND status = 'ACTIVE'`, inviteUsername).Scan(&inviteUsername, &inviteEmail)
		if errors.Is(err, sql.ErrNoRows) {
			failure(c, http.StatusNotFound, "USER_NOT_FOUND", "用户名不存在或已停用", nil)
			return
		}
		if err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询用户", nil)
			return
		}
	} else {
		address, err := mail.ParseAddress(strings.TrimSpace(request.Email))
		if err != nil {
			failure(c, http.StatusBadRequest, "INVALID_EMAIL", "邮箱无效", nil)
			return
		}
		inviteEmail = strings.ToLower(address.Address)
	}
	// 邀请令牌原文只用于交付，数据库保存摘要和过期时间
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		failure(c, http.StatusInternalServerError, "TOKEN_ERROR", "无法生成邀请", nil)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	var id int64
	err = s.db.QueryRowContext(c.Request.Context(), `
INSERT INTO workspace_invitations (workspace_id, email_normalized, role, token_hash, status, invited_by, expires_at)
VALUES ($1, $2, $3, $4, 'PENDING', $5, $6) RETURNING id`, workspaceID, inviteEmail, request.Role, hash[:], currentUserID(c), expiresAt).Scan(&id)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建邀请", nil)
		return
	}

	s.recordAudit(c, workspaceID, currentUserID(c), "INVITATION_CREATED", "invitation", id, gin.H{"email": inviteEmail, "username": inviteUsername, "role": request.Role})
	resultData := gin.H{"id": uint64(id), "token": token, "email": inviteEmail, "role": request.Role, "expiresAt": expiresAt}
	if inviteUsername != "" {
		resultData["username"] = inviteUsername
	}
	success(c, http.StatusCreated, resultData)
}

// acceptInvitation 校验邀请归属与有效性，在事务中更新成员关系和邀请状态
func (s *server) acceptInvitation(c *gin.Context) {
	var request struct {
		Token        string `json:"token"`
		InvitationID uint64 `json:"invitationId"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || (request.Token == "" && request.InvitationID == 0) {
		failure(c, http.StatusBadRequest, "INVALID_INVITATION", "邀请令牌无效", nil)
		return
	}
	hash := sha256.Sum256([]byte(request.Token))
	userID := currentUserID(c)
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法接受邀请", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var invitationID, workspaceID, invitedBy uint64
	var email, role, status string
	var expiresAt time.Time
	// 支持令牌或邀请 ID 定位，行锁与邮箱归属检查共同保护接受流程
	query := `SELECT id, workspace_id, email_normalized, role, status, expires_at, invited_by FROM workspace_invitations WHERE token_hash = $1 FOR UPDATE`
	args := []any{hash[:]}
	if request.InvitationID != 0 {
		query = `SELECT id, workspace_id, email_normalized, role, status, expires_at, invited_by FROM workspace_invitations WHERE id = $1 FOR UPDATE`
		args = []any{request.InvitationID}
	}
	err = tx.QueryRowContext(c.Request.Context(), query, args...).Scan(&invitationID, &workspaceID, &email, &role, &status, &expiresAt, &invitedBy)
	if err != nil || status != "PENDING" || time.Now().UTC().After(expiresAt) {
		failure(c, http.StatusConflict, "INVITATION_UNAVAILABLE", "邀请不存在、已处理或已过期", nil)
		return
	}
	var userEmail string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT email_normalized FROM users WHERE id = $1`, userID).Scan(&userEmail); err != nil || userEmail != email {
		failure(c, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH", "邀请邮箱与当前账号不匹配", nil)
		return
	}
	// 接受时检查邀请人的当前权限，旧邀请不能绕过角色降级或成员禁用
	var inviterRole string
	if err := tx.QueryRowContext(c.Request.Context(), "SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2 AND status = 'ACTIVE' FOR UPDATE", workspaceID, invitedBy).Scan(&inviterRole); err != nil || (inviterRole != "OWNER" && inviterRole != "ADMIN") || (role == "ADMIN" && inviterRole != "OWNER") {
		failure(c, http.StatusForbidden, "INVITATION_UNAVAILABLE", "邀请人的权限已失效，请重新邀请", nil)
		return
	}
	var existingRole, existingStatus string
	existingErr := tx.QueryRowContext(c.Request.Context(), "SELECT role, status FROM workspace_members WHERE workspace_id = $1 AND user_id = $2 FOR UPDATE", workspaceID, userID).Scan(&existingRole, &existingStatus)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法检查成员状态", nil)
		return
	}
	if existingErr == nil && (existingRole == "OWNER" || existingStatus != "REMOVED") {
		failure(c, http.StatusConflict, "MEMBERSHIP_EXISTS", "成员关系已存在，请通过成员管理修改", nil)
		return
	}
	if existingErr == nil {
		_, err = tx.ExecContext(c.Request.Context(), "UPDATE workspace_members SET role = $1, status = 'ACTIVE', joined_at = statement_timestamp(), left_at = NULL WHERE workspace_id = $2 AND user_id = $3", role, workspaceID, userID)
	} else {
		_, err = tx.ExecContext(c.Request.Context(), "INSERT INTO workspace_members (workspace_id, user_id, role, status) VALUES ($1, $2, $3, 'ACTIVE')", workspaceID, userID, role)
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法加入工作区", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE workspace_invitations SET status = 'ACCEPTED', accepted_by = $1, accepted_at = CURRENT_TIMESTAMP WHERE id = $2`, userID, invitationID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法完成邀请", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交成员关系", nil)
		return
	}
	s.recordAudit(c, workspaceID, userID, "INVITATION_ACCEPTED", "invitation", invitationID, gin.H{"role": role})
	success(c, http.StatusOK, gin.H{"workspaceId": workspaceID, "role": role})
}

// requireWorkspaceMember 验证当前用户的有效成员关系，返回权限错误或数据库错误响应
func (s *server) requireWorkspaceMember(c *gin.Context, workspaceID uint64) (membership, bool) {
	member, err := s.membership(workspaceID, currentUserID(c))
	if errors.Is(err, sql.ErrNoRows) {
		failure(c, http.StatusForbidden, "FORBIDDEN", "无权访问工作区", nil)
		return membership{}, false
	}
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", fmt.Sprintf("检查工作区权限失败: %v", err), nil)
		return membership{}, false
	}
	return member, true
}
