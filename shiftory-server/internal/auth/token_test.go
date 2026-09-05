package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestTokenManagerIssuesAndParsesTypedTokens(t *testing.T) {
	manager := testTokenManager(t)
	pair, err := manager.IssuePair(42, "family-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("issue pair: %v", err)
	}
	access, err := manager.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse access: %v", err)
	}
	if access.UserID != 42 || access.Type != TokenTypeAccess {
		t.Fatalf("unexpected access claims: %+v", access)
	}
	refresh, err := manager.ParseRefresh(pair.RefreshToken)
	if err != nil {
		t.Fatalf("parse refresh: %v", err)
	}
	if refresh.UserID != 42 || refresh.FamilyID != "family-1" || refresh.Type != TokenTypeRefresh {
		t.Fatalf("unexpected refresh claims: %+v", refresh)
	}
	if _, err := manager.ParseAccess(pair.RefreshToken); err == nil {
		t.Fatal("refresh token was accepted as access token")
	}
}

func TestTokenManagerRejectsWrongIssuerAndSignature(t *testing.T) {
	manager := testTokenManager(t)
	pair, err := manager.IssuePair(42, "family-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("issue pair: %v", err)
	}

	_, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	wrongSignature := NewTokenManager(otherPrivate, otherPrivate.Public().(ed25519.PublicKey), "kid-2", "shiftory", "shiftory-web", time.Minute, time.Hour)
	if _, err := wrongSignature.ParseAccess(pair.AccessToken); err == nil {
		t.Fatal("token with wrong signature was accepted")
	}

	wrongIssuer := NewTokenManager(manager.privateKey, manager.publicKey, "kid-1", "other-issuer", "shiftory-web", time.Minute, time.Hour)
	if _, err := wrongIssuer.ParseAccess(pair.AccessToken); err == nil {
		t.Fatal("token with wrong issuer was accepted")
	}
}

func testTokenManager(t *testing.T) *TokenManager {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}
	return NewTokenManager(privateKey, publicKey, "kid-1", "shiftory", "shiftory-web", 15*time.Minute, 7*24*time.Hour)
}
