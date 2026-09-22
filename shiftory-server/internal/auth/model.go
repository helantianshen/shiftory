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

// UserStatus 表示账号是否允许认证访问
type UserStatus string

const (
	UserActive   UserStatus = "ACTIVE"
	UserDisabled UserStatus = "DISABLED"
)

// User 保存账号资料与认证字段，HTTP 响应需要显式排除敏感字段
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

// RegisterInput 承载注册时提交的账号资料与明文密码
type RegisterInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

// LoginInput 承载用户名或邮箱及用于本次验证的密码
type LoginInput struct {
	Login    string
	Password string
}

// ClientInfo 记录会话关联的客户端 IP 和浏览器标识
type ClientInfo struct {
	IPAddress string
	UserAgent string
}

// ProfileInput 承载允许用户编辑的展示名与头像地址
type ProfileInput struct {
	DisplayName string
	AvatarURL   string
}

// PasswordInput 承载改密时验证的当前密码及新密码
type PasswordInput struct {
	CurrentPassword string
	NewPassword     string
}

// RefreshRecord 保存令牌摘要、所属族和消费撤销时间，不保存明文令牌
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
