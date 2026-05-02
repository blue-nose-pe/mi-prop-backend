package query

import (
	"context"
	"testing"

	"users_service/internal/core/domain"
	"users_service/internal/core/testutil"
)

func TestPermissionQuery_HasPermission(t *testing.T) {
	perms := testutil.NewPermissionRepoMock()
	perms.CodesByUser["u1"] = []string{"users.view", "colegios.view"}

	h := NewPermissionHandler(perms)
	ctx := context.Background()

	cases := []struct {
		code string
		want bool
	}{
		{"users.view", true},
		{"colegios.view", true},
		{"users.delete", false},
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
	perms := testutil.NewPermissionRepoMock()
	perms.CodesByUser["u1"] = []string{"users.view"}

	h := NewPermissionHandler(perms)
	codes, err := h.ListUserPermissions(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) != 1 || codes[0] != "users.view" {
		t.Errorf("expected [users.view], got %v", codes)
	}
}

// Verifica que un usuario sin permisos retorne lista vacía sin error.
func TestPermissionQuery_NoPermissions(t *testing.T) {
	perms := testutil.NewPermissionRepoMock()
	h := NewPermissionHandler(perms)

	codes, err := h.ListUserPermissions(context.Background(), domain.UserID("u-anonimo"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) != 0 {
		t.Errorf("expected empty list, got %v", codes)
	}
}
