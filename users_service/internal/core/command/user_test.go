package command

import (
	"context"
	"errors"
	"testing"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/core/testutil"
)

func newUserHandlerWithMocks() (*UserHandler, *testutil.UserRepoMock, *testutil.UserCacheMock, *testutil.HasherMock) {
	users := testutil.NewUserRepoMock()
	cache := testutil.NewUserCacheMock()
	hasher := &testutil.HasherMock{}
	return NewUserHandler(users, nil, cache, hasher, nil), users, cache, hasher
}

func TestUserHandler_Create_Ok(t *testing.T) {
	h, users, cache, _ := newUserHandlerWithMocks()
	ctx := context.Background()

	out, err := h.Create(ctx, ports.CreateUserInput{
		Email:          "Foo@Example.COM",
		Password:       "secret123",
		FirstName:      "Foo",
		LastName:       "Bar",
		DocumentNumber: "12345678",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if out.Email != domain.Email("foo@example.com") {
		t.Errorf("email not normalized: got %q", out.Email)
	}
	if out.PasswordHash != "hashed:secret123" {
		t.Errorf("password not hashed: got %q", out.PasswordHash)
	}
	if !out.Active {
		t.Error("new user should be active")
	}
	if _, err := users.FindByID(ctx, out.ID); err != nil {
		t.Errorf("user not persisted: %v", err)
	}
	if _, err := cache.Get(ctx, out.ID); err != nil {
		t.Errorf("user not cached: %v", err)
	}
}

func TestUserHandler_Create_RejectsInvalidEmail(t *testing.T) {
	h, _, _, _ := newUserHandlerWithMocks()

	_, err := h.Create(context.Background(), ports.CreateUserInput{
		Email:    "not-an-email",
		Password: "secret123",
	})
	if !errors.Is(err, domain.ErrInvalidEmail) {
		t.Errorf("expected ErrInvalidEmail, got %v", err)
	}
}

func TestUserHandler_Create_RejectsWeakPassword(t *testing.T) {
	h, _, _, _ := newUserHandlerWithMocks()

	_, err := h.Create(context.Background(), ports.CreateUserInput{
		Email:    "foo@example.com",
		Password: "short",
	})
	if !errors.Is(err, domain.ErrWeakPassword) {
		t.Errorf("expected ErrWeakPassword, got %v", err)
	}
}

func TestUserHandler_Create_RejectsDuplicateEmail(t *testing.T) {
	h, users, _, _ := newUserHandlerWithMocks()
	users.Seed(&domain.User{
		ID:    "u1",
		Email: domain.Email("dup@example.com"),
	})

	_, err := h.Create(context.Background(), ports.CreateUserInput{
		Email:    "dup@example.com",
		Password: "secret123",
	})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Errorf("expected ErrEmailTaken, got %v", err)
	}
}

func TestUserHandler_Create_RejectsDuplicateDocument(t *testing.T) {
	h, users, _, _ := newUserHandlerWithMocks()
	users.Seed(&domain.User{
		ID:             "u1",
		Email:          domain.Email("a@example.com"),
		DocumentNumber: "12345678",
	})

	_, err := h.Create(context.Background(), ports.CreateUserInput{
		Email:          "b@example.com",
		Password:       "secret123",
		DocumentNumber: "12345678",
	})
	if !errors.Is(err, domain.ErrDocumentTaken) {
		t.Errorf("expected ErrDocumentTaken, got %v", err)
	}
}

func TestUserHandler_Update_PartialFields(t *testing.T) {
	h, users, cache, _ := newUserHandlerWithMocks()
	users.Seed(&domain.User{
		ID:        "u1",
		Email:     domain.Email("foo@example.com"),
		FirstName: "Old",
		LastName:  "Bar",
	})

	out, err := h.Update(context.Background(), ports.UpdateUserInput{
		ID:        "u1",
		FirstName: "New",
		// LastName empty → no change
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.FirstName != "New" {
		t.Errorf("FirstName: want New, got %q", out.FirstName)
	}
	if out.LastName != "Bar" {
		t.Errorf("LastName should not have changed: got %q", out.LastName)
	}
	// Update debe invalidar la cache.
	if len(cache.Deleted) != 1 || cache.Deleted[0] != "u1" {
		t.Errorf("expected cache invalidated for u1, got %v", cache.Deleted)
	}
}

func TestUserHandler_Deactivate_OK(t *testing.T) {
	h, users, cache, _ := newUserHandlerWithMocks()
	users.Seed(&domain.User{ID: "u1", Email: "foo@example.com", Active: true})

	if err := h.Deactivate(context.Background(), "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := users.FindByID(context.Background(), "u1")
	if u.Active {
		t.Error("expected user inactive after deactivate")
	}
	if len(cache.Deleted) != 1 {
		t.Error("cache must be invalidated on deactivate")
	}
}

func TestUserHandler_Deactivate_NotFound(t *testing.T) {
	h, _, _, _ := newUserHandlerWithMocks()
	err := h.Deactivate(context.Background(), "missing")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}
