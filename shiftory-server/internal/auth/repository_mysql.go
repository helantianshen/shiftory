package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

type MySQLRepository struct{ db *sql.DB }

func NewMySQLRepository(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

func (r *MySQLRepository) CreateUser(ctx context.Context, user User) (User, error) {
	result, err := r.db.ExecContext(ctx, `
INSERT INTO users
    (username, username_normalized, email, email_normalized, display_name, password_hash, avatar_url, status, password_changed_at)
VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		user.Username, user.UsernameNormalized, user.Email, user.EmailNormalized, user.DisplayName,
		user.PasswordHash, user.AvatarURL, user.Status, user.PasswordChangedAt)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("read user id: %w", err)
	}
	return r.FindUserByID(ctx, uint64(id))
}

func (r *MySQLRepository) FindUserByLogin(ctx context.Context, login string) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `
SELECT id, username, username_normalized, email, email_normalized, display_name, password_hash,
       COALESCE(avatar_url, ''), status, password_changed_at, created_at, updated_at
FROM users WHERE username_normalized = ? OR email_normalized = ? LIMIT 1`, login, login))
}

func (r *MySQLRepository) FindUserByID(ctx context.Context, id uint64) (User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `
SELECT id, username, username_normalized, email, email_normalized, display_name, password_hash,
       COALESCE(avatar_url, ''), status, password_changed_at, created_at, updated_at
FROM users WHERE id = ?`, id))
}

func (r *MySQLRepository) UpdateProfile(ctx context.Context, id uint64, displayName, avatarURL string) (User, error) {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET display_name = ?, avatar_url = NULLIF(?, '') WHERE id = ?`, displayName, avatarURL, id)
	if err != nil {
		return User{}, fmt.Errorf("update user profile: %w", err)
	}
	return r.FindUserByID(ctx, id)
}

func (r *MySQLRepository) UpdatePassword(ctx context.Context, id uint64, passwordHash string, changedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, password_changed_at = ? WHERE id = ?`, passwordHash, changedAt, id)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE user_id = ?`, changedAt, id)
	if err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

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

func (r *MySQLRepository) StoreRefresh(ctx context.Context, record RefreshRecord) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO auth_refresh_tokens
    (user_id, family_id, jwt_id, token_hash, expires_at, user_agent, ip_address, created_at)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)`,
		record.UserID, record.FamilyID, record.JWTID, record.TokenHash[:], record.ExpiresAt,
		record.UserAgent, record.IPAddress, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	return nil
}

func (r *MySQLRepository) ConsumeRefresh(ctx context.Context, hash [32]byte, usedAt time.Time, replacementJWTID string) (RefreshRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var record RefreshRecord
	var hashBytes []byte
	var used, revoked sql.NullTime
	var replacement sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT user_id, family_id, jwt_id, token_hash, expires_at, used_at, revoked_at,
       replaced_by_jwt_id, COALESCE(ip_address, ''), COALESCE(user_agent, ''), created_at
FROM auth_refresh_tokens WHERE token_hash = ? FOR UPDATE`, hash[:]).Scan(
		&record.UserID, &record.FamilyID, &record.JWTID, &hashBytes, &record.ExpiresAt, &used, &revoked,
		&replacement, &record.IPAddress, &record.UserAgent, &record.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("lock refresh token: %w", err)
	}
	copy(record.TokenHash[:], hashBytes)
	if revoked.Valid || time.Now().UTC().After(record.ExpiresAt) {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if used.Valid {
		return record, ErrRefreshReplay
	}
	result, err := tx.ExecContext(ctx, `
UPDATE auth_refresh_tokens SET used_at = ?, replaced_by_jwt_id = ?
WHERE token_hash = ? AND used_at IS NULL AND revoked_at IS NULL`, usedAt, replacementJWTID, hash[:])
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

func (r *MySQLRepository) RevokeFamily(ctx context.Context, familyID string, revokedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE auth_refresh_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE family_id = ?`, revokedAt, familyID)
	if err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	return nil
}
