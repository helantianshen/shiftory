package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceRegisterLoginRefreshAndReplayProtection(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := NewService(repo, NewPasswordHasher(PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}), testTokenManager(t))

	registered, err := service.Register(ctx, RegisterInput{
		Username: " Alice ", Email: "Alice@Example.com", DisplayName: "Alice", Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if registered.Username != "Alice" || registered.Email != "Alice@Example.com" {
		t.Fatalf("unexpected normalized public user: %+v", registered)
	}

	pair, user, err := service.Login(ctx, LoginInput{Login: "alice@example.com", Password: "correct horse battery staple"}, ClientInfo{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if user.ID != registered.ID || pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("unexpected login result: user=%+v pair=%+v", user, pair)
	}

	rotated, err := service.Refresh(ctx, pair.RefreshToken, ClientInfo{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := service.Refresh(ctx, pair.RefreshToken, ClientInfo{}); !errors.Is(err, ErrRefreshReplay) {
		t.Fatalf("expected replay detection, got %v", err)
	}
	if _, err := service.Refresh(ctx, rotated.RefreshToken, ClientInfo{}); !errors.Is(err, ErrRefreshRevoked) {
		t.Fatalf("replay must revoke the full family, got %v", err)
	}
}

func TestServiceRejectsDuplicateAndDisabledUsers(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := NewService(repo, NewPasswordHasher(PasswordParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}), testTokenManager(t))
	input := RegisterInput{Username: "alice", Email: "alice@example.com", DisplayName: "Alice", Password: "correct horse battery staple"}
	user, err := service.Register(ctx, input)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := service.Register(ctx, input); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	disabled := repo.users[user.ID]
	disabled.Status = UserDisabled
	repo.users[user.ID] = disabled
	if _, _, err := service.Login(ctx, LoginInput{Login: "alice", Password: input.Password}, ClientInfo{}); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("expected disabled rejection, got %v", err)
	}
}

type memoryRepository struct {
	users    map[uint64]User
	byLogin  map[string]uint64
	tokens   map[[32]byte]RefreshRecord
	families map[string]bool
	nextID   uint64
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{users: map[uint64]User{}, byLogin: map[string]uint64{}, tokens: map[[32]byte]RefreshRecord{}, families: map[string]bool{}, nextID: 1}
}

func (r *memoryRepository) CreateUser(_ context.Context, user User) (User, error) {
	if _, ok := r.byLogin[user.UsernameNormalized]; ok {
		return User{}, ErrUserExists
	}
	if _, ok := r.byLogin[user.EmailNormalized]; ok {
		return User{}, ErrUserExists
	}
	user.ID = r.nextID
	r.nextID++
	r.users[user.ID] = user
	r.byLogin[user.UsernameNormalized] = user.ID
	r.byLogin[user.EmailNormalized] = user.ID
	return user, nil
}

func (r *memoryRepository) FindUserByLogin(_ context.Context, login string) (User, error) {
	id, ok := r.byLogin[login]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return r.users[id], nil
}

func (r *memoryRepository) FindUserByID(_ context.Context, id uint64) (User, error) {
	user, ok := r.users[id]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (r *memoryRepository) UpdateProfile(_ context.Context, id uint64, displayName, avatarURL string) (User, error) {
	user, ok := r.users[id]
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	user.DisplayName, user.AvatarURL = displayName, avatarURL
	r.users[id] = user
	return user, nil
}

func (r *memoryRepository) UpdatePassword(_ context.Context, id uint64, passwordHash string, changedAt time.Time) error {
	user, ok := r.users[id]
	if !ok {
		return ErrInvalidCredentials
	}
	user.PasswordHash, user.PasswordChangedAt = passwordHash, changedAt
	r.users[id] = user
	return nil
}

func (r *memoryRepository) StoreRefresh(_ context.Context, record RefreshRecord) error {
	r.tokens[record.TokenHash] = record
	return nil
}

func (r *memoryRepository) ConsumeRefresh(_ context.Context, hash [32]byte, usedAt time.Time, replacementJWTID string) (RefreshRecord, error) {
	record, ok := r.tokens[hash]
	if !ok {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if r.families[record.FamilyID] {
		return RefreshRecord{}, ErrRefreshRevoked
	}
	if record.UsedAt != nil {
		return record, ErrRefreshReplay
	}
	record.UsedAt = &usedAt
	record.ReplacedByJWTID = replacementJWTID
	r.tokens[hash] = record
	return record, nil
}

func (r *memoryRepository) RevokeFamily(_ context.Context, familyID string, _ time.Time) error {
	r.families[familyID] = true
	return nil
}
