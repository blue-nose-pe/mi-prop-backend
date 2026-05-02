package mssqladapter

import (
	"context"
	"database/sql"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type PermissionRepo struct {
	db *sql.DB
}

var _ ports.PermissionRepository = (*PermissionRepo)(nil)

func NewPermissionRepo(db *sql.DB) *PermissionRepo { return &PermissionRepo{db: db} }

// FindCodesByUserID: user → user_permission_group → permission_group_permission → permission.
// Solo permisos y grupos activos.
func (r *PermissionRepo) FindCodesByUserID(ctx context.Context, userID domain.UserID) ([]string, error) {
	const q = `
		SELECT DISTINCT p.code
		  FROM user_permission_group upg
		  JOIN permission_group pg              ON pg.id = upg.permission_group_id AND pg.active = 1
		  JOIN permission_group_permission pgp  ON pgp.permission_group_id = pg.id
		  JOIN permission p                     ON p.id = pgp.permission_id AND p.active = 1
		 WHERE upg.user_id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	rows, err := r.db.QueryContext(ctx, q, string(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := make([]string, 0, 16)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

func (r *PermissionRepo) GroupExists(ctx context.Context, groupID uint32) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS (SELECT 1 FROM permission_group WHERE id = @p1 AND active = 1) THEN 1 ELSE 0 END`,
		groupID).Scan(&exists)
	return exists == 1, err
}

// AssignGroupToUser es idempotente: si la asignación ya existe, no falla.
// T-SQL no tiene ON CONFLICT — usamos un patrón equivalente con NOT EXISTS.
func (r *PermissionRepo) AssignGroupToUser(ctx context.Context, userID domain.UserID, groupID uint32) error {
	const q = `
		IF NOT EXISTS (
		    SELECT 1 FROM user_permission_group
		     WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		       AND permission_group_id = @p2
		)
		BEGIN
		    INSERT INTO user_permission_group (user_id, permission_group_id)
		    VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), @p2);
		END`
	_, err := r.db.ExecContext(ctx, q, string(userID), groupID)
	return err
}

func (r *PermissionRepo) RevokeGroupFromUser(ctx context.Context, userID domain.UserID, groupID uint32) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM user_permission_group
		  WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND permission_group_id = @p2`,
		string(userID), groupID)
	return err
}
