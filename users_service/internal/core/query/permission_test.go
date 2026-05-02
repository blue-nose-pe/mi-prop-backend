package query

import (
	"context"
	"testing"

	"users_service/internal/core/domain"
	"users_service/internal/core/testutil"
)

func TestPermissionQuery_HasPermission(t *testing.T) {
	users := testutil.NewUserRepoMock()
	users.Seed(&domain.User{ID: "u1", Email: "a@example.com", Active: true})
	perms := testutil.NewPermissionRepoMock()
	perms.CodesByUser["u1"] = []string{"db_users.users.read", "db_users.school.read"}

	h := NewPermissionHandler(users, perms)
	ctx := context.Background()

	cases := []struct {
		code string
		want bool
	}{
		{"db_users.users.read", true},
		{"db_users.school.read", true},
		{"db_users.users.write", false},
	}
	for _, c := range cases {
		got, err := h.HasPermission(ctx, "u1", c.code)
		if err != nil {
			t.Fatalf("unexpected error for code %q: %v", c.code, err)
		}
		if got != c.want {
			t.Errorf("HasPermission(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}

func TestPermissionQuery_ListUserPermissions(t *testing.T) {
	users := testutil.NewUserRepoMock()
	users.Seed(&domain.User{ID: "u1", Email: "a@example.com", Active: true})
	perms := testutil.NewPermissionRepoMock()
	perms.CodesByUser["u1"] = []string{"db_users.users.read"}

	h := NewPermissionHandler(users, perms)
	codes, err := h.ListUserPermissions(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) != 1 || codes[0] != "db_users.users.read" {
		t.Errorf("expected [db_users.users.read], got %v", codes)
	}
}

// Un user sin permisos retorna lista vacía sin error.
func TestPermissionQuery_NoPermissions(t *testing.T) {
	users := testutil.NewUserRepoMock()
	users.Seed(&domain.User{ID: "u-anonimo", Email: "x@example.com", Active: true})
	perms := testutil.NewPermissionRepoMock()
	h := NewPermissionHandler(users, perms)

	codes, err := h.ListUserPermissions(context.Background(), domain.UserID("u-anonimo"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) != 0 {
		t.Errorf("expected empty list, got %v", codes)
	}
}

// Bypass de superadmin: HasPermission retorna true para cualquier code,
// y ListUserPermissions retorna ["*"] como sentinel.
func TestPermissionQuery_SuperadminBypass(t *testing.T) {
	users := testutil.NewUserRepoMock()
	users.Seed(&domain.User{
		ID: "super1", Email: "root@ucsp.edu.pe", Active: true,
		IsSuperadmin: true,
	})
	perms := testutil.NewPermissionRepoMock()
	// Notar: perms.CodesByUser["super1"] está VACÍO. El bypass debe
	// dar true igual.
	h := NewPermissionHandler(users, perms)
	ctx := context.Background()

	for _, code := range []string{"db_users.permission.write", "hubspot.jobs.write", "anything.at.all"} {
		ok, err := h.HasPermission(ctx, "super1", code)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if !ok {
			t.Errorf("superadmin should have %q", code)
		}
	}

	codes, err := h.ListUserPermissions(ctx, "super1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(codes) != 1 || codes[0] != "*" {
		t.Errorf("superadmin permissions should be [\"*\"], got %v", codes)
	}
}
