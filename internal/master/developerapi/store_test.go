package developerapi

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/storage"
)

func testStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	wal := false
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(t.TempDir(), "master.db"), BusyTimeout: "5s", WAL: &wal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db, time.UTC, "https://mirror.example"), db
}

func TestRotateTokenStoresOnlyHashAndInvalidatesOldToken(t *testing.T) {
	store, db := testStore(t)
	first, err := store.RotateToken(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Token, "mmdev_") || first.TokenPrefix == "" {
		t.Fatalf("unexpected token result: %+v", first)
	}
	var storedHash, storedPrefix string
	if err := db.QueryRow("SELECT token_hash, token_prefix FROM project_developer_tokens WHERE project_id = 'p1'").
		Scan(&storedHash, &storedPrefix); err != nil {
		t.Fatal(err)
	}
	if storedHash == first.Token || storedHash != tokenHash(first.Token) {
		t.Fatal("database must store only the token hash")
	}
	if storedPrefix != first.TokenPrefix {
		t.Fatalf("stored prefix=%q want %q", storedPrefix, first.TokenPrefix)
	}
	if projectID, _, err := store.Authenticate(context.Background(), first.Token); err != nil || projectID != "p1" {
		t.Fatalf("first token authentication failed: project=%q err=%v", projectID, err)
	}

	second, err := store.RotateToken(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if second.Token == first.Token {
		t.Fatal("token rotation returned the same token")
	}
	if _, _, err := store.Authenticate(context.Background(), first.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token must be invalid after rotation, got %v", err)
	}
	if projectID, _, err := store.Authenticate(context.Background(), second.Token); err != nil || projectID != "p1" {
		t.Fatalf("rotated token authentication failed: project=%q err=%v", projectID, err)
	}
}

func TestDailyQuotaStopsAtLimitAndResetsOnNextDay(t *testing.T) {
	store, _ := testStore(t)
	store.DailyLimit = 2
	result, err := store.RotateToken(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	for wantUsed := 1; wantUsed <= 2; wantUsed++ {
		usage, err := store.Consume(context.Background(), "p1", result.Token, now)
		if err != nil {
			t.Fatal(err)
		}
		if usage.Used != wantUsed || usage.Remaining != 2-wantUsed {
			t.Fatalf("usage=%+v at call %d", usage, wantUsed)
		}
	}
	usage, err := store.Consume(context.Background(), "p1", result.Token, now)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third call err=%v want rate limit", err)
	}
	if usage.Used != 2 || usage.Remaining != 0 {
		t.Fatalf("limited usage=%+v", usage)
	}
	rotated, err := store.RotateToken(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if usage, err = store.Consume(context.Background(), "p1", rotated.Token, now); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("token rotation must not reset daily quota: usage=%+v err=%v", usage, err)
	}

	nextDay := now.Add(24 * time.Hour)
	usage, err = store.Consume(context.Background(), "p1", rotated.Token, nextDay)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Used != 1 || usage.Remaining != 1 {
		t.Fatalf("next-day usage=%+v", usage)
	}
}

func TestRevokeMissingRemovesOnlyDeletedProjectTokens(t *testing.T) {
	store, _ := testStore(t)
	p1, _ := store.RotateToken(context.Background(), "p1")
	p2, _ := store.RotateToken(context.Background(), "p2")
	if err := store.RevokeMissing(context.Background(),
		map[string]struct{}{"p1": {}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Authenticate(context.Background(), p1.Token); err != nil {
		t.Fatalf("kept token unexpectedly revoked: %v", err)
	}
	if _, _, err := store.Authenticate(context.Background(), p2.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("deleted project token still valid: %v", err)
	}
}
