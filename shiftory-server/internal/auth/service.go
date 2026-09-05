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

type Repository interface {
	CreateUser(context.Context, User) (User, error)
	FindUserByLogin(context.Context, string) (User, error)
	FindUserByID(context.Context, uint64) (User, error)
	UpdateProfile(context.Context, uint64, string, string) (User, error)
	UpdatePassword(context.Context, uint64, string, time.Time) error
	StoreRefresh(context.Context, RefreshRecord) error
	ConsumeRefresh(context.Context, [32]byte, time.Time, string) (RefreshRecord, error)
	RevokeFamily(context.Context, string, time.Time) error
}

type Service struct {
	repository Repository
	passwords  *PasswordHasher
	tokens     *TokenManager
	now        func() time.Time
}

func NewService(repository Repository, passwords *PasswordHasher, tokens *TokenManager) *Service {
	return &Service{repository: repository, passwords: passwords, tokens: tokens, now: func() time.Time { return time.Now().UTC() }}
}

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
	pair, err := s.tokens.IssuePair(user.ID, "", s.now())
	if err != nil {
		return TokenPair{}, User{}, err
	}
	if err := s.storeRefresh(ctx, user.ID, pair, client); err != nil {
		return TokenPair{}, User{}, err
	}
	return pair, user, nil
}

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
	_, err = s.repository.ConsumeRefresh(ctx, HashToken(raw), now, replacement.RefreshJWTID)
	if errors.Is(err, ErrRefreshReplay) {
		_ = s.repository.RevokeFamily(ctx, claims.FamilyID, now)
		return TokenPair{}, ErrRefreshReplay
	}
	if err != nil {
		return TokenPair{}, ErrRefreshRevoked
	}
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

func (s *Service) Logout(ctx context.Context, raw string) error {
	claims, err := s.tokens.ParseRefresh(raw)
	if err != nil {
		return nil
	}
	return s.repository.RevokeFamily(ctx, claims.FamilyID, s.now())
}

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

func (s *Service) UpdateProfile(ctx context.Context, id uint64, input ProfileInput) (User, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	avatarURL := strings.TrimSpace(input.AvatarURL)
	if displayName == "" || len([]rune(displayName)) > 80 || len(avatarURL) > 1024 {
		return User{}, ErrInvalidProfile
	}
	return s.repository.UpdateProfile(ctx, id, displayName, avatarURL)
}

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

func (s *Service) storeRefresh(ctx context.Context, userID uint64, pair TokenPair, client ClientInfo) error {
	return s.repository.StoreRefresh(ctx, RefreshRecord{
		UserID: userID, FamilyID: pair.FamilyID, JWTID: pair.RefreshJWTID,
		TokenHash: HashToken(pair.RefreshToken), ExpiresAt: pair.RefreshExpiry,
		IPAddress: client.IPAddress, UserAgent: client.UserAgent, CreatedAt: s.now(),
	})
}

func normalizeLogin(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
