package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{2,63}$`)

// Repository 封装用户及刷新令牌持久化，消费刷新令牌需要具备并发排他语义
type Repository interface {
	// CreateUser 保存用户并返回包含持久化标识的记录
	CreateUser(context.Context, User) (User, error)
	// FindUserByLogin 按规范化用户名或邮箱检索用户
	FindUserByLogin(context.Context, string) (User, error)
	// FindUserByID 按用户标识检索完整账号记录
	FindUserByID(context.Context, uint64) (User, error)
	// UpdateProfile 更新允许编辑的展示名与头像地址
	UpdateProfile(context.Context, uint64, string, string) (User, error)
	// UpdatePassword 保存密码摘要及修改时间
	UpdatePassword(context.Context, uint64, string, time.Time) error
	// StoreRefresh 保存刷新令牌摘要与轮换元数据
	StoreRefresh(context.Context, RefreshRecord) error
	// ConsumeRefresh 排他消费令牌摘要，并关联替代令牌标识
	ConsumeRefresh(context.Context, [32]byte, time.Time, string) (RefreshRecord, error)
	// RevokeFamily 撤销同一刷新令牌族的记录
	RevokeFamily(context.Context, string, time.Time) error
}

// Service 组织认证业务与令牌轮换，通过仓储访问用户和会话状态
type Service struct {
	repository Repository
	passwords  *PasswordHasher
	tokens     *TokenManager
	now        func() time.Time
}

// NewService 组合用户仓储、密码散列器与令牌管理器，并使用 UTC 时钟
func NewService(repository Repository, passwords *PasswordHasher, tokens *TokenManager) *Service {
	return &Service{repository: repository, passwords: passwords, tokens: tokens, now: func() time.Time { return time.Now().UTC() }}
}

// Register 校验账号资料和密码长度后创建有效用户，不签发登录令牌
func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !usernamePattern.MatchString(input.Username) || input.DisplayName == "" {
		return User{}, ErrInvalidProfile
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || !strings.EqualFold(address.Address, input.Email) {
		return User{}, ErrInvalidProfile
	}
	if len([]rune(input.Password)) < 10 {
		return User{}, ErrWeakPassword
	}
	// 资料校验通过后才执行密码散列，持久化时同时保存规范化检索字段
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	now := s.now()
	return s.repository.CreateUser(ctx, User{
		Username: input.Username, UsernameNormalized: normalizeLogin(input.Username),
		Email: input.Email, EmailNormalized: normalizeLogin(input.Email), DisplayName: input.DisplayName,
		PasswordHash: hash, Status: UserActive, PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now,
	})
}

// Login 核对凭据和账号状态，持久化刷新令牌摘要后返回令牌对与用户
func (s *Service) Login(ctx context.Context, input LoginInput, client ClientInfo) (TokenPair, User, error) {
	user, err := s.repository.FindUserByLogin(ctx, normalizeLogin(input.Login))
	if err != nil {
		return TokenPair{}, User{}, ErrInvalidCredentials
	}
	ok, err := s.passwords.Verify(user.PasswordHash, input.Password)
	if err != nil || !ok {
		return TokenPair{}, User{}, ErrInvalidCredentials
	}
	if user.Status != UserActive {
		return TokenPair{}, User{}, ErrUserDisabled
	}
	// 仅在凭据和账号状态均有效时签发令牌，刷新摘要保存失败则不交付令牌
	pair, err := s.tokens.IssuePair(user.ID, "", s.now())
	if err != nil {
		return TokenPair{}, User{}, err
	}
	if err := s.storeRefresh(ctx, user.ID, pair, client); err != nil {
		return TokenPair{}, User{}, err
	}
	return pair, user, nil
}

// Refresh 消费旧刷新令牌并签发同族替代令牌，检测到重放时尝试撤销整个令牌族
func (s *Service) Refresh(ctx context.Context, raw string, client ClientInfo) (TokenPair, error) {
	claims, err := s.tokens.ParseRefresh(raw)
	if err != nil {
		return TokenPair{}, ErrRefreshRevoked
	}
	now := s.now()
	replacement, err := s.tokens.IssuePair(claims.UserID, claims.FamilyID, now)
	if err != nil {
		return TokenPair{}, err
	}
	// 消费旧令牌的事务先行提交，后续替代令牌保存失败不会恢复旧令牌
	_, err = s.repository.ConsumeRefresh(ctx, HashToken(raw), now, replacement.RefreshJWTID)
	if errors.Is(err, ErrRefreshReplay) {
		// 同一刷新令牌再次使用表示令牌链可能泄露，整组令牌必须一并撤销
		_ = s.repository.RevokeFamily(ctx, claims.FamilyID, now)
		return TokenPair{}, ErrRefreshReplay
	}
	if err != nil {
		return TokenPair{}, ErrRefreshRevoked
	}
	// 轮换期间重新核对账号状态，停用用户不能取得替代令牌
	user, err := s.repository.FindUserByID(ctx, claims.UserID)
	if err != nil || user.Status != UserActive {
		_ = s.repository.RevokeFamily(ctx, claims.FamilyID, now)
		return TokenPair{}, ErrUserDisabled
	}
	if err := s.storeRefresh(ctx, user.ID, replacement, client); err != nil {
		return TokenPair{}, err
	}
	return replacement, nil
}

// Logout 撤销刷新令牌所属令牌族，无法解析的令牌按已登出处理
func (s *Service) Logout(ctx context.Context, raw string) error {
	claims, err := s.tokens.ParseRefresh(raw)
	if err != nil {
		// 登出保持幂等，失效或已撤销的 Cookie 不向客户端暴露额外状态
		return nil
	}
	return s.repository.RevokeFamily(ctx, claims.FamilyID, s.now())
}

// CurrentUser 返回仍处于有效状态的用户，供请求鉴权使用
func (s *Service) CurrentUser(ctx context.Context, id uint64) (User, error) {
	user, err := s.repository.FindUserByID(ctx, id)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	if user.Status != UserActive {
		return User{}, ErrUserDisabled
	}
	return user, nil
}

// UpdateProfile 校验展示名与头像地址长度后保存个人资料
func (s *Service) UpdateProfile(ctx context.Context, id uint64, input ProfileInput) (User, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	avatarURL := strings.TrimSpace(input.AvatarURL)
	if displayName == "" || len([]rune(displayName)) > 80 || len(avatarURL) > 1024 {
		return User{}, ErrInvalidProfile
	}
	return s.repository.UpdateProfile(ctx, id, displayName, avatarURL)
}

// ChangePassword 验证当前密码后保存新摘要，并由仓储撤销用户的刷新令牌
func (s *Service) ChangePassword(ctx context.Context, id uint64, input PasswordInput) error {
	if len([]rune(input.NewPassword)) < 10 {
		return ErrWeakPassword
	}
	user, err := s.repository.FindUserByID(ctx, id)
	if err != nil {
		return ErrInvalidCredentials
	}
	valid, err := s.passwords.Verify(user.PasswordHash, input.CurrentPassword)
	if err != nil || !valid {
		return ErrInvalidCredentials
	}
	hash, err := s.passwords.Hash(input.NewPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	now := s.now()
	if err := s.repository.UpdatePassword(ctx, id, hash, now); err != nil {
		return err
	}
	return nil
}

// storeRefresh 持久化刷新令牌摘要、令牌族和客户端信息，不保存令牌原文
func (s *Service) storeRefresh(ctx context.Context, userID uint64, pair TokenPair, client ClientInfo) error {
	return s.repository.StoreRefresh(ctx, RefreshRecord{
		UserID: userID, FamilyID: pair.FamilyID, JWTID: pair.RefreshJWTID,
		TokenHash: HashToken(pair.RefreshToken), ExpiresAt: pair.RefreshExpiry,
		IPAddress: client.IPAddress, UserAgent: client.UserAgent, CreatedAt: s.now(),
	})
}

// normalizeLogin 去除首尾空白并统一小写，使用户名和邮箱使用相同检索形式
func normalizeLogin(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
