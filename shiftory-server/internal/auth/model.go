package auth

import (
	"errors"
	"time"
)

var (
	ErrUserExists         = errors.New("username or email already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user is disabled")
	ErrInvalidProfile     = errors.New("invalid user profile")
	ErrWeakPassword       = errors.New("password must contain at least 10 characters")
	ErrRefreshReplay      = errors.New("refresh token replay detected")
	ErrRefreshRevoked     = errors.New("refresh token is revoked")
)

type UserStatus string

const (
	UserActive   UserStatus = "ACTIVE"
	UserDisabled UserStatus = "DISABLED"
)

type User struct {
	ID                 uint64
	Username           string
	UsernameNormalized string
	Email              string
	EmailNormalized    string
	DisplayName        string
	PasswordHash       string
	AvatarURL          string
	Status             UserStatus
	PasswordChangedAt  time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type RegisterInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

type LoginInput struct {
	Login    string
	Password string
}

type ClientInfo struct {
	IPAddress string
	UserAgent string
}

type ProfileInput struct {
	DisplayName string
	AvatarURL   string
}

type PasswordInput struct {
	CurrentPassword string
	NewPassword     string
}

type RefreshRecord struct {
	UserID          uint64
	FamilyID        string
	JWTID           string
	TokenHash       [32]byte
	ExpiresAt       time.Time
	UsedAt          *time.Time
	RevokedAt       *time.Time
	ReplacedByJWTID string
	IPAddress       string
	UserAgent       string
	CreatedAt       time.Time
}
