package httpserver

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"shiftory-server/internal/auth"
	"shiftory-server/internal/platform/config"
)

func TestPostgresConcurrentScheduleWrites(t *testing.T) {
	db := openCleanTestDatabase(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cfg, _ := config.Load()
	router, err := New(Dependencies{DB: db, Config: cfg, Tokens: auth.NewTokenManager(priv, pub, "fixture", cfg.JWTIssuer, cfg.JWTAudience, time.Hour, time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	alice := registerAndLogin(t, router, "concurrent", "concurrent@example.com")
	for _, body := range []string{`{"status":"REST","note":"create","segments":[],"version":0}`, `{"status":"REST","note":"update","segments":[],"version":1}`} {
		start := make(chan struct{})
		results := make(chan int, 8)
		var workers sync.WaitGroup
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				req := httptest.NewRequest("PUT", "/api/v1/me/schedules/2026-10-02", bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+alice.AccessToken)
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				results <- out.Code
			}()
		}
		close(start)
		workers.Wait()
		close(results)
		success := 0
		for status := range results {
			switch status {
			case 200:
				success++
			case 409:
			default:
				t.Errorf("unexpected concurrent write status %d", status)
			}
		}
		if success != 1 {
			t.Fatalf("expected one successful write, got %d", success)
		}
	}
	var days, revisions int
	if err := db.QueryRow("SELECT COUNT(*) FROM schedule_days").Scan(&days); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM schedule_revisions").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if days != 1 || revisions != 2 {
		t.Fatalf("partial/duplicate writes: days=%d revisions=%d", days, revisions)
	}
}

func TestPostgresConcurrentRefreshConsumption(t *testing.T) {
	db := openCleanTestDatabase(t)
	repo := auth.NewPostgresRepository(db)
	ctx := context.Background()
	user, err := repo.CreateUser(ctx, auth.User{Username: "rotate", UsernameNormalized: "rotate", Email: "rotate@example.com", EmailNormalized: "rotate@example.com", DisplayName: "Rotate", PasswordHash: "hash", Status: "ACTIVE", PasswordChangedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("test-refresh"))
	if err := repo.StoreRefresh(ctx, auth.RefreshRecord{UserID: user.ID, FamilyID: "test-family", JWTID: "test-token", TokenHash: hash, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := repo.ConsumeRefresh(ctx, hash, time.Now().UTC(), "replacement")
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, auth.ErrRefreshReplay) {
			t.Errorf("unexpected rotation error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("expected one refresh consumption, got %d", success)
	}
}
