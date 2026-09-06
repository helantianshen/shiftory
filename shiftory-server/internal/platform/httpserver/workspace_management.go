package httpserver

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *server) listMyInvitations(c *gin.Context) {
	rows, err := s.db.QueryContext(c.Request.Context(), `SELECT i.id, i.workspace_id, w.name, i.role, i.expires_at FROM workspace_invitations i JOIN users u ON u.email_normalized = i.email_normalized JOIN workspaces w ON w.id = i.workspace_id WHERE u.id = ? AND i.status = 'PENDING' AND i.expires_at > UTC_TIMESTAMP(6) ORDER BY i.created_at DESC`, currentUserID(c))
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询待处理邀请", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, workspaceID uint64
		var name, role string
		var expires time.Time
		if err := rows.Scan(&id, &workspaceID, &name, &role, &expires); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取邀请", nil)
			return
		}
		items = append(items, gin.H{"id": id, "workspaceId": workspaceID, "workspaceName": name, "role": role, "expiresAt": expires})
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) listInvitations(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(member) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以查看邀请", nil)
		}
		return
	}
	_, _ = s.db.ExecContext(c.Request.Context(), `UPDATE workspace_invitations SET status = 'EXPIRED' WHERE workspace_id = ? AND status = 'PENDING' AND expires_at < UTC_TIMESTAMP(6)`, workspaceID)
	rows, err := s.db.QueryContext(c.Request.Context(), `
SELECT i.id, i.email_normalized, COALESCE(u.username, ''), i.role, i.status, i.invited_by, i.expires_at, i.accepted_by, i.accepted_at, i.created_at
FROM workspace_invitations i LEFT JOIN users u ON u.email_normalized = i.email_normalized
WHERE i.workspace_id = ? ORDER BY i.created_at DESC`, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法查询邀请", nil)
		return
	}
	defer rows.Close()
	items := make([]gin.H, 0)
	for rows.Next() {
		var id, invitedBy uint64
		var email, username, role, status string
		var expiresAt, createdAt time.Time
		var acceptedBy sql.NullInt64
		var acceptedAt sql.NullTime
		if err := rows.Scan(&id, &email, &username, &role, &status, &invitedBy, &expiresAt, &acceptedBy, &acceptedAt, &createdAt); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取邀请", nil)
			return
		}
		item := gin.H{"id": id, "email": email, "role": role, "status": status, "invitedBy": invitedBy, "expiresAt": expiresAt, "createdAt": createdAt}
		if username != "" {
			item["username"] = username
		}
		if acceptedBy.Valid {
			item["acceptedBy"] = uint64(acceptedBy.Int64)
			item["acceptedAt"] = acceptedAt.Time
		}
		items = append(items, item)
	}
	success(c, http.StatusOK, gin.H{"items": items})
}

func (s *server) revokeInvitation(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	invitationID, ok := parseID(c, "invitationId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || !requireAdmin(member) {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有管理员可以撤销邀请", nil)
		}
		return
	}
	result, err := s.db.ExecContext(c.Request.Context(), `UPDATE workspace_invitations SET status = 'REVOKED' WHERE id = ? AND workspace_id = ? AND status = 'PENDING'`, invitationID, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法撤销邀请", nil)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		failure(c, http.StatusConflict, "INVITATION_NOT_REVOCABLE", "邀请不存在或已处理", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "INVITATION_REVOKED", "invitation", invitationID, nil)
	success(c, http.StatusOK, gin.H{"id": invitationID, "status": "REVOKED"})
}

func (s *server) transferWorkspace(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || member.Role != "OWNER" {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有工作区所有者可以转让工作区", nil)
		}
		return
	}
	var request struct {
		NewOwnerUserID uint64 `json:"newOwnerUserId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.NewOwnerUserID == currentUserID(c) {
		failure(c, http.StatusBadRequest, "INVALID_NEW_OWNER", "新所有者无效", nil)
		return
	}
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法转让工作区", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var targetStatus string
	if err := tx.QueryRowContext(c.Request.Context(), `SELECT status FROM workspace_members WHERE workspace_id = ? AND user_id = ? FOR UPDATE`, workspaceID, request.NewOwnerUserID).Scan(&targetStatus); err != nil || targetStatus != "ACTIVE" {
		failure(c, http.StatusBadRequest, "INVALID_NEW_OWNER", "新所有者必须是当前有效成员", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE workspace_members SET role = 'ADMIN' WHERE workspace_id = ? AND user_id = ? AND role = 'OWNER'`, workspaceID, currentUserID(c)); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新原所有者角色", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE workspace_members SET role = 'OWNER' WHERE workspace_id = ? AND user_id = ?`, workspaceID, request.NewOwnerUserID); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新新所有者角色", nil)
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), `UPDATE workspaces SET owner_user_id = ? WHERE id = ? AND owner_user_id = ?`, request.NewOwnerUserID, workspaceID, currentUserID(c)); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法更新工作区所有者", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交工作区转让", nil)
		return
	}
	s.recordAudit(c, workspaceID, currentUserID(c), "WORKSPACE_TRANSFERRED", "workspace", workspaceID, gin.H{"newOwnerUserId": request.NewOwnerUserID})
	success(c, http.StatusOK, gin.H{"id": workspaceID, "ownerUserId": request.NewOwnerUserID})
}

func (s *server) deleteWorkspace(c *gin.Context) {
	workspaceID, ok := parseID(c, "workspaceId")
	if !ok {
		return
	}
	member, ok := s.requireWorkspaceMember(c, workspaceID)
	if !ok || member.Role != "OWNER" {
		if ok {
			failure(c, http.StatusForbidden, "FORBIDDEN", "只有工作区所有者可以解散工作区", nil)
		}
		return
	}
	var request struct {
		ConfirmationName string `json:"confirmationName" binding:"required"`
	}
	var name string
	if err := s.db.QueryRowContext(c.Request.Context(), `SELECT name FROM workspaces WHERE id = ?`, workspaceID).Scan(&name); err != nil {
		failure(c, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "工作区不存在", nil)
		return
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.ConfirmationName != name {
		failure(c, http.StatusBadRequest, "CONFIRMATION_MISMATCH", "请输入完整工作区名称以确认解散", nil)
		return
	}
	fileRows, err := s.db.QueryContext(c.Request.Context(), `SELECT f.storage_key FROM import_files f JOIN import_jobs j ON j.id = f.import_job_id WHERE j.workspace_id = ?`, workspaceID)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取工作区文件", nil)
		return
	}
	storageKeys := make([]string, 0)
	for fileRows.Next() {
		var key string
		if err := fileRows.Scan(&key); err != nil {
			fileRows.Close()
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法读取工作区文件", nil)
			return
		}
		storageKeys = append(storageKeys, key)
	}
	fileRows.Close()
	tx, err := s.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法解散工作区", nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	deletions := []string{
		"DELETE FROM schedule_revisions WHERE workspace_id = ?",
		"DELETE ss FROM schedule_segments ss JOIN schedule_days sd ON sd.id = ss.schedule_day_id WHERE sd.workspace_id = ?",
		"DELETE FROM schedule_days WHERE workspace_id = ?",
		"DELETE ii FROM import_items ii JOIN import_jobs ij ON ij.id = ii.import_job_id WHERE ij.workspace_id = ?",
		"DELETE f FROM import_files f JOIN import_jobs ij ON ij.id = f.import_job_id WHERE ij.workspace_id = ?",
		"DELETE FROM import_jobs WHERE workspace_id = ?",
		"DELETE FROM shift_aliases WHERE workspace_id = ?",
		"DELETE FROM shifts WHERE workspace_id = ?",
		"DELETE FROM workspace_invitations WHERE workspace_id = ?",
		"DELETE FROM audit_logs WHERE workspace_id = ?",
		"UPDATE user_preferences SET current_workspace_id = NULL WHERE current_workspace_id = ?",
		"DELETE FROM workspace_members WHERE workspace_id = ?",
	}
	for _, query := range deletions {
		if _, err := tx.ExecContext(c.Request.Context(), query, workspaceID); err != nil {
			failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法清理工作区数据", gin.H{"reason": err.Error()})
			return
		}
	}
	if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM workspaces WHERE id = ? AND owner_user_id = ?`, workspaceID, currentUserID(c)); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法删除工作区", nil)
		return
	}
	if err := tx.Commit(); err != nil {
		failure(c, http.StatusInternalServerError, "DATABASE_ERROR", "无法提交工作区删除", nil)
		return
	}
	for _, key := range storageKeys {
		_ = s.store.Delete(c.Request.Context(), key)
	}
	details, _ := json.Marshal(gin.H{"workspaceId": workspaceID, "name": name})
	_, _ = s.db.ExecContext(c.Request.Context(), `INSERT INTO audit_logs (workspace_id, actor_user_id, action, target_type, target_id, details) VALUES (NULL, ?, 'WORKSPACE_DELETED', 'workspace', ?, ?)`, currentUserID(c), workspaceID, details)
	success(c, http.StatusOK, gin.H{"id": workspaceID, "deleted": true})
}
