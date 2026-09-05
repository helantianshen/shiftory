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

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

type Claims struct {
	UserID   uint64    `json:"uid"`
	Type     TokenType `json:"typ"`
	FamilyID string    `json:"family_id,omitempty"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken   string    `json:"accessToken"`
	RefreshToken  string    `json:"-"`
	AccessExpiry  time.Time `json:"accessExpiresAt"`
	RefreshExpiry time.Time `json:"refreshExpiresAt"`
	AccessJWTID   string    `json:"-"`
	RefreshJWTID  string    `json:"-"`
	FamilyID      string    `json:"-"`
}

type TokenManager struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenManager(privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, keyID, issuer, audience string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{privateKey: privateKey, publicKey: publicKey, keyID: keyID, issuer: issuer, audience: audience, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (m *TokenManager) IssuePair(userID uint64, familyID string, now time.Time) (TokenPair, error) {
	if familyID == "" {
		familyID = uuid.NewString()
	}
	accessID, refreshID := uuid.NewString(), uuid.NewString()
	accessExpiry, refreshExpiry := now.Add(m.accessTTL), now.Add(m.refreshTTL)
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

func (m *TokenManager) sign(claims Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = m.keyID
	signed, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

func (m *TokenManager) ParseAccess(raw string) (Claims, error) {
	return m.parse(raw, TokenTypeAccess)
}

func (m *TokenManager) ParseRefresh(raw string) (Claims, error) {
	return m.parse(raw, TokenTypeRefresh)
}

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
	if claims.Type != expected || claims.UserID == 0 || claims.Subject != strconv.FormatUint(claims.UserID, 10) {
		return Claims{}, errors.New("invalid jwt token type or subject")
	}
	if expected == TokenTypeRefresh && claims.FamilyID == "" {
		return Claims{}, errors.New("refresh jwt missing family id")
	}
	return claims, nil
}

func HashToken(raw string) [32]byte { return sha256.Sum256([]byte(raw)) }
