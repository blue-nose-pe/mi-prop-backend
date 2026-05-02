package query

import (
	"context"
	"errors"
	"testing"

	"users_service/internal/core/domain"
	"users_service/internal/core/testutil"
)

func TestUserQuery_Get_CacheHit(t *testing.T) {
	users := testutil.NewUserRepoMock()
	cache := testutil.NewUserCacheMock()
	perms := testutil.NewPermissionRepoMock()
	cached := &domain.User{ID: "u1", Email: "cached@example.com", Active: true}
	_ = cache.Set(context.Background(), cached)

	h := NewUserHandler(users, cache, perms)

	out, err := h.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Email != "cached@example.com" {
		t.Errorf("expected user from cache, got %q", out.Email)
	}
}

func TestUserQuery_Get_CacheMissReadsRepoAndCaches(t *testing.T) {
	users := testutil.NewUserRepoMock()
	cache := testutil.NewUserCacheMock()
	perms := testutil.NewPermissionRepoMock()
	users.Seed(&domain.User{ID: "u1", Email: "a@example.com", Active: true})

	h := NewUserHandler(users, cache, perms)

	out, err := h.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected user, got nil")
	}
	// Después del miss debe haberse cacheado (hit en próxima lectura).
	if u, _ := cache.Get(context.Background(), "u1"); u == nil {
		t.Error("expected cache populated after miss")
	}
}

func TestUserQuery_Get_NotFound(t *testing.T) {
	users := testutil.NewUserRepoMock()
	cache := testutil.NewUserCacheMock()
	perms := testutil.NewPermissionRepoMock()
	h := NewUserHandler(users, cache, perms)

	_, err := h.Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserQuery_GetByEmail_NormalizesAndValidates(t *testing.T) {
	users := testutil.NewUserRepoMock()
	cache := testutil.NewUserCacheMock()
	perms := testutil.NewPermissionRepoMock()
	users.Seed(&domain.User{ID: "u1", Email: "foo@example.com", Active: true})

	h := NewUserHandler(users, cache, perms)

	out, err := h.GetByEmail(context.Background(), "  FoO@Example.COM ")
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if out.ID != "u1" {
		t.Errorf("expected u1, got %s", out.ID)
	}

	_, err = h.GetByEmail(context.Background(), "garbage")
	if !errors.Is(err, domain.ErrInvalidEmail) {
		t.Errorf("expected ErrInvalidEmail, got %v", err)
	}
}
