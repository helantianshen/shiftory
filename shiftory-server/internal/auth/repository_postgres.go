package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresRepository 使用共享 Postgres 连接池实现本模块的持久化操作
type PostgresRepository struct{ db *sql.DB }

// NewPostgresRepository 使用调用方管理的数据库连接池创建认证仓储
func NewPostgresRepository(db *sql.DB) *PostgresRepository { return &PostgresRepository{db: db} }

// CreateUser 插入用户并回读数据库记录，将唯一键冲突映射为账号已存在
func (r *PostgresRepository) CreateUser(ctx context.Context, user User) (User, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO users
    (username, username_normalized, email, email_normalized, display_name, password_hash, avatar_url, status, password_changed_at)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9) RETURNING id`,
		user.Username, user.UsernameNormalized, user.Email, user.EmailNormalized, user.DisplayName,
		user.PasswordHash, user.AvatarURL, user.Status, user.PasswordChangedAt).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return r.FindUserByID(ctx, uint64(id))
}

// FindUserByLogin 按规范化用户名或邮箱查询用户
func (r *PostgresRepository) FindUserByLogin(ctx context.Context, login string) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `
SELECT id, username, username_normalized, email, email_normalized, display_name, password_hash,
       COALESCE(avatar_url, ''), status, password_changed_at, created_at, updated_at
FROM users WHERE username_normalized = $1 OR email_normalized = $2 LIMIT 1`, login, login))
}

// FindUserByID 按用户 ID 查询账号，状态是否可用由服务层判断
func (r *PostgresRepository) FindUserByID(ctx context.Context, id uint64) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `
SELECT id, username, username_normalized, email, email_normalized, display_name, password_hash,
       COALESCE(avatar_url, ''), status, password_changed_at, created_at, updated_at
FROM users WHERE id = $1`, id))
}

// UpdateProfile 保存展示名和头像地址，空头像按 SQL NULL 存储
func (r *PostgresRepository) UpdateProfile(ctx context.Context, id uint64, displayName, avatarURL string) (User, error) {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET display_name = $1, avatar_url = NULLIF($2, '') WHERE id = $3`, displayName, avatarURL, id)
	if err != nil {
		return User{}, fmt.Errorf("update user profile: %w", err)
	}
	return r.FindUserByID(ctx, id)
}

// UpdatePassword 依次更新密码与撤销刷新令牌，两次写入不在同一事务中
func (r *PostgresRepository) UpdatePassword(ctx context.Context, id uint64, passwordHash string, changedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET password_hash = $1, password_changed_at = $2 WHERE id = $3`, passwordHash, changedAt, id)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, $1) WHERE user_id = $2`, changedAt, id)
	if err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}

// rowScanner 统一单行查询与结果集的字段解码入口
type rowScanner interface{ Scan(...any) error }

// scanUser 解码统一的用户查询列，将无记录映射为无效凭据
func scanUser(row rowScanner) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.UsernameNormalized, &user.Email, &user.EmailNormalized,
		&user.DisplayName, &user.PasswordHash, &user.AvatarURL, &user.Status, &user.PasswordChangedAt,
		&user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return user, nil
}

// StoreRefresh 保存刷新令牌的哈希与轮换元数据
func (r *PostgresRepository) StoreRefresh(ctx context.Context, record RefreshRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO auth_refresh_tokens
    (user_id, family_id, jwt_id, token_hash, expires_at, user_agent, ip_address, created_at)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)`,
		record.UserID, record.FamilyID, record.JWTID, record.TokenHash[:], record.ExpiresAt,
		record.UserAgent, record.IPAddress, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	return nil
}

// ConsumeRefresh 锁定并消费一次刷新令牌，返回被消费记录或重放、撤销错误
func (r *PostgresRepository) ConsumeRefresh(ctx context.Context, hash [32]byte, usedAt time.Time, replacementJWTID string) (RefreshRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var record RefreshRecord
	var hashBytes []byte
	var used, revoked sql.NullTime
	var replacement sql.NullString
	// FOR UPDATE 串行化同一令牌的消费，只有首个请求可以完成轮换
	err = tx.QueryRowContext(ctx, `
SELECT user_id, family_id, jwt_id, token_hash, expires_at, used_at, revoked_at,
       replaced_by_jwt_id, COALESCE(ip_address, ''), COALESCE(user_agent, ''), created_at
FROM auth_refresh_tokens WHERE token_hash = $1 FOR UPDATE`, hash[:]).Scan(
		&record.UserID, &record.FamilyID, &record.JWTID, &hashBytes, &record.ExpiresAt, &used, &revoked,
		&replacement, &record.IPAddress, &record.UserAgent, &record.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("lock refresh token: %w", err)
	}
	// 锁内区分已撤销、过期和已使用状态，让服务层识别令牌重放
	copy(record.TokenHash[:], hashBytes)
	if revoked.Valid || time.Now().UTC().After(record.ExpiresAt) {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if used.Valid {
		return record, ErrRefreshReplay
	}
	// 条件更新记录消费时间与替代标识，本事务不负责插入替代令牌
	result, err := tx.ExecContext(ctx, `
UPDATE auth_refresh_tokens SET used_at = $1, replaced_by_jwt_id = $2
WHERE token_hash = $3 AND used_at IS NULL AND revoked_at IS NULL`, usedAt, replacementJWTID, hash[:])
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("consume refresh token: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return record, ErrRefreshReplay
	}
	if err := tx.Commit(); err != nil {
		return RefreshRecord{}, fmt.Errorf("commit refresh rotation: %w", err)
	}
	record.UsedAt = &usedAt
	record.ReplacedByJWTID = replacementJWTID
	return record, nil
}

// RevokeFamily 撤销指定令牌族，保留已有撤销时间以支持重复调用
func (r *PostgresRepository) RevokeFamily(ctx context.Context, familyID string, revokedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, $1) WHERE family_id = $2`, revokedAt, familyID)
	if err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	return nil
}
