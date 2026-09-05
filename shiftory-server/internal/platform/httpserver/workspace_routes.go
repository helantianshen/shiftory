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

type membership struct {
	UserID uint64
	Role   string
	Status string
}

func (s *server) membership(workspaceID, userID uint64) (membership, error) {
	var member membership
	err := s.db.QueryRow(`
SELECT user_id, role, status FROM workspace_members
WHERE workspace_id = ? AND user_id = ? AND status = 'ACTIVE'`, workspaceID, userID).Scan(&member.UserID, &member.Role, &member.Status)
	return member, err
}

func requireAdmin(member membership) bool { return member.Role == "OWNER" || member.Role == "ADMIN" }

func parseID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		failure(c, http.StatusBadRequest, "INVALID_ID", "资源 ID 无效", nil)
		return 0, false
	}
	return id, true
}

type createWorkspaceRequest struct {
	Name     string `json:"name" binding:"required"`
	Timezone string `json:"timezone" binding:"required"`
}

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
	result, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO workspaces (name, timezone, owner_user_id, created_by) VALUES (?, ?, ?, ?)`, strings.TrimSpace(request.Name), request.Timezone, userID, userID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建工作区", nil)
		return
	}
	id, _ := result.LastInsertId()
	if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO workspace_members (workspace_id, user_id, role, status) VALUES (?, ?, 'OWNER', 'ACTIVE')`, id, userID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建所有者关系", nil)
		return
	}
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
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, shift.name, shift.code, shift.start, shift.end, shift.cross, shift.color, index, userID); err != nil {
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

func (s *server) listWorkspaces(c *gin.Context) {
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT w.id, w.name, w.timezone, m.role
FROM workspaces w JOIN workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = ? AND m.status = 'ACTIVE' ORDER BY w.created_at`, currentUserID(c))
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
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT timezone FROM workspaces WHERE id = ?`, workspaceID).Scan(&timezone); err != nil {
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
        WHERE sd.workspace_id = m.workspace_id AND sd.user_id = m.user_id
          AND sd.work_date BETWEEN ? AND ?),
       (SELECT MAX(ij.created_at) FROM import_jobs ij
        WHERE ij.workspace_id = m.workspace_id AND ij.target_user_id = m.user_id
          AND ij.state IN ('COMPLETED', 'ROLLED_BACK'))
FROM workspace_members m JOIN users u ON u.id = m.user_id
WHERE m.workspace_id = ? ORDER BY FIELD(m.role, 'OWNER', 'ADMIN', 'MEMBER'), u.display_name`, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"), workspaceID)
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

type invitationRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role" binding:"required"`
}

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
	if err := c.ShouldBindJSON(&request); err != nil || (request.Role != "MEMBER" && request.Role != "ADMIN") {
		failure(c, http.StatusBadRequest, "INVALID_INVITATION", "邀请信息无效", nil)
		return
	}
	address, err := mail.ParseAddress(strings.TrimSpace(request.Email))
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_EMAIL", "邮箱无效", nil)
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		failure(c, http.StatusInternalServerError, "TOKEN_ERROR", "无法生成邀请", nil)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	result, err := s.db.ExecContext(c.Request.Context(), `
INSERT INTO workspace_invitations (workspace_id, email_normalized, role, token_hash, status, invited_by, expires_at)
VALUES (?, ?, ?, ?, 'PENDING', ?, ?)`, workspaceID, strings.ToLower(address.Address), request.Role, hash[:], currentUserID(c), expiresAt)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法创建邀请", nil)
		return
	}
	id, _ := result.LastInsertId()
	s.recordAudit(c, workspaceID, currentUserID(c), "INVITATION_CREATED", "invitation", id, gin.H{"email": address.Address, "role": request.Role})
	success(c, http.StatusCreated, gin.H{"id": uint64(id), "token": token, "email": address.Address, "role": request.Role, "expiresAt": expiresAt})
}

func (s *server) acceptInvitation(c *gin.Context) {
	var request struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
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
	var invitationID, workspaceID uint64
	var email, role, status string
	var expiresAt time.Time
	err = tx.QueryRowContext(c.Request.Context(), `
SELECT id, workspace_id, email_normalized, role, status, expires_at
FROM workspace_invitations WHERE token_hash = ? FOR UPDATE`, hash[:]).Scan(&invitationID, &workspaceID, &email, &role, &status, &expiresAt)
	if err != nil || status != "PENDING" || time.Now().UTC().After(expiresAt) {
		failure(c, http.StatusConflict, "INVITATION_UNAVAILABLE", "邀请不存在、已处理或已过期", nil)
		return
	}
	var userEmail string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT email_normalized FROM users WHERE id = ?`, userID).Scan(&userEmail); err != nil || userEmail != email {
		failure(c, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH", "邀请邮箱与当前账号不匹配", nil)
		return
	}
	_, err = tx.ExecContext(c.Request.Context(), `
INSERT INTO workspace_members (workspace_id, user_id, role, status, joined_at, left_at)
VALUES (?, ?, ?, 'ACTIVE', CURRENT_TIMESTAMP(6), NULL)
ON DUPLICATE KEY UPDATE role = VALUES(role), status = 'ACTIVE', joined_at = CURRENT_TIMESTAMP(6), left_at = NULL`, workspaceID, userID, role)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法加入工作区", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE workspace_invitations SET status = 'ACCEPTED', accepted_by = ?, accepted_at = CURRENT_TIMESTAMP(6) WHERE id = ?`, userID, invitationID); err != nil {
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
