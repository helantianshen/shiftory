package auth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenType 区分访问令牌与刷新令牌的使用场景
type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

// Claims 携带用户身份和令牌用途，不包含工作区角色
type Claims struct {
	UserID   uint64    `json:"uid"`
	Type     TokenType `json:"typ"`
	FamilyID string    `json:"family_id,omitempty"`
	jwt.RegisteredClaims
}

// TokenPair 中 RefreshToken 及各内部标识不会进入 JSON 响应
// RefreshToken 仅通过 HttpOnly Cookie 交付，AccessToken 由响应体交付
type TokenPair struct {
	AccessToken   string    `json:"accessToken"`
	RefreshToken  string    `json:"-"`
	AccessExpiry  time.Time `json:"accessExpiresAt"`
	RefreshExpiry time.Time `json:"refreshExpiresAt"`
	AccessJWTID   string    `json:"-"`
	RefreshJWTID  string    `json:"-"`
	FamilyID      string    `json:"-"`
}

// TokenManager 持有签名密钥及校验参数，数据库撤销状态由认证服务检查
type TokenManager struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenManager 配置 Ed25519 签名密钥、签发方、受众和两类令牌有效期
func NewTokenManager(privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, keyID, issuer, audience string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{privateKey: privateKey, publicKey: publicKey, keyID: keyID, issuer: issuer, audience: audience, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// IssuePair 签发访问与刷新令牌，空 familyID 会创建新的刷新令牌族
func (m *TokenManager) IssuePair(userID uint64, familyID string, now time.Time) (TokenPair, error) {
	if familyID == "" {
		familyID = uuid.NewString()
	}
	accessID, refreshID := uuid.NewString(), uuid.NewString()
	accessExpiry, refreshExpiry := now.Add(m.accessTTL), now.Add(m.refreshTTL)
	// NotBefore 向前容忍五秒，用于吸收签发端与校验端的轻微时钟偏差
	access, err := m.sign(Claims{
		UserID: userID,
		Type:   TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: m.issuer, Subject: strconv.FormatUint(userID, 10), Audience: jwt.ClaimStrings{m.audience},
			ExpiresAt: jwt.NewNumericDate(accessExpiry), NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt: jwt.NewNumericDate(now), ID: accessID,
		},
	})
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := m.sign(Claims{
		UserID: userID, Type: TokenTypeRefresh, FamilyID: familyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: m.issuer, Subject: strconv.FormatUint(userID, 10), Audience: jwt.ClaimStrings{m.audience},
			ExpiresAt: jwt.NewNumericDate(refreshExpiry), NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt: jwt.NewNumericDate(now), ID: refreshID,
		},
	})
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken: access, RefreshToken: refresh, AccessExpiry: accessExpiry, RefreshExpiry: refreshExpiry,
		AccessJWTID: accessID, RefreshJWTID: refreshID, FamilyID: familyID,
	}, nil
}

// sign 使用 EdDSA 签名声明并在 JWT 头中写入密钥标识
func (m *TokenManager) sign(claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = m.keyID
	signed, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

// ParseAccess 校验 JWT 签名及访问令牌类型，返回可信身份声明
func (m *TokenManager) ParseAccess(raw string) (Claims, error) {
	return m.parse(raw, TokenTypeAccess)
}

// ParseRefresh 校验 JWT 签名、刷新类型及令牌族，不检查数据库中的撤销状态
func (m *TokenManager) ParseRefresh(raw string) (Claims, error) {
	return m.parse(raw, TokenTypeRefresh)
}

// parse 校验签名、密钥标识、标准时间声明与用户身份一致性
func (m *TokenManager) parse(raw string, expected TokenType) (Claims, error) {
	claims := Claims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Header["kid"] != m.keyID {
			return nil, errors.New("unknown jwt key id")
		}
		return m.publicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithIssuer(m.issuer), jwt.WithAudience(m.audience), jwt.WithExpirationRequired(), jwt.WithNotBeforeRequired())
	if err != nil || !token.Valid {
		return Claims{}, fmt.Errorf("validate jwt: %w", err)
	}
	// 签名和标准声明通过后，仍需检查用途、用户 ID 与 subject 相互一致
	if claims.Type != expected || claims.UserID == 0 || claims.Subject != strconv.FormatUint(claims.UserID, 10) {
		return Claims{}, errors.New("invalid jwt token type or subject")
	}
	if expected == TokenTypeRefresh && claims.FamilyID == "" {
		return Claims{}, errors.New("refresh jwt missing family id")
	}
	return claims, nil
}

// HashToken 生成刷新令牌的持久化摘要，数据库不保存令牌原文
func HashToken(raw string) [32]byte {
	return sha256.Sum256([]byte(raw))
}
