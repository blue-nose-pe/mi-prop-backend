package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"

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

// ----------------------------------------------------------------------
// Administración de permission_group (CRUD a runtime via API).
// ----------------------------------------------------------------------

// CreateGroup inserta un nuevo grupo y, opcionalmente, asocia los permisos
// recibidos. Toda la operación va dentro de una transacción para evitar
// estados inconsistentes (grupo creado sin sus permisos por un fallo
// parcial). Devuelve el id INT generado vía OUTPUT INSERTED.id.
func (r *PermissionRepo) CreateGroup(
	ctx context.Context,
	code, name, description string,
	permissionIDs []uint32,
) (uint32, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	const insertQ = `
		INSERT INTO permission_group (code, name, description)
		OUTPUT INSERTED.id
		VALUES (@p1, @p2, NULLIF(@p3, ''))`

	var newID int64
	if err := tx.QueryRowContext(ctx, insertQ, code, name, description).Scan(&newID); err != nil {
		return 0, mapPermGroupDuplicate(err)
	}

	// Asociar permisos (idempotente con NOT EXISTS por si vienen duplicados).
	const linkQ = `
		IF NOT EXISTS (
		    SELECT 1 FROM permission_group_permission
		     WHERE permission_group_id = @p1 AND permission_id = @p2
		)
		BEGIN
		    INSERT INTO permission_group_permission (permission_group_id, permission_id)
		    VALUES (@p1, @p2);
		END`
	for _, pid := range permissionIDs {
		if _, err := tx.ExecContext(ctx, linkQ, newID, pid); err != nil {
			// Si el permission_id no existe, la FK levantará error 547.
			return 0, mapPermFK(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uint32(newID), nil
}

// UpdateGroup edita name/description del grupo. El code es inmutable
// (es la "clave estable" usada por seeds y por los joins externos).
func (r *PermissionRepo) UpdateGroup(ctx context.Context, id uint32, name, description string) error {
	const q = `
		UPDATE permission_group
		   SET name = @p1, description = NULLIF(@p2, '')
		 WHERE id = @p3`
	res, err := r.db.ExecContext(ctx, q, name, description, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrPermGroupNotFound
	}
	return nil
}

// DeleteGroup hace borrado físico (cascade a permission_group_permission).
// REGLA: si el grupo tiene users asignados (user_permission_group) NO se
// borra; el caller debe revocar primero. Esto evita huérfanos lógicos
// y obliga al admin a confirmar el cleanup.
func (r *PermissionRepo) DeleteGroup(ctx context.Context, id uint32) error {
	hasUsers, err := r.GroupHasUsers(ctx, id)
	if err != nil {
		return err
	}
	if hasUsers {
		return domain.ErrPermGroupHasUsers
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM permission_group_permission WHERE permission_group_id = @p1`, id,
	); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM permission_group WHERE id = @p1`, id,
	)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrPermGroupNotFound
	}

	return tx.Commit()
}

// FindGroupByID retorna el grupo + sus permisos asociados.
// Una sola query con LEFT JOIN: si el grupo no tiene permisos, igual
// devolvemos el grupo (Permissions = []).
func (r *PermissionRepo) FindGroupByID(ctx context.Context, id uint32) (*domain.PermissionGroup, error) {
	const q = `
		SELECT g.id, g.code, g.name, ISNULL(g.description, ''), g.active,
		       g.created_at, g.updated_at,
		       p.id, p.scope, p.code, p.name, ISNULL(p.description, ''), p.active,
		       p.created_at, p.updated_at
		  FROM permission_group g
		  LEFT JOIN permission_group_permission pgp ON pgp.permission_group_id = g.id
		  LEFT JOIN permission p                    ON p.id = pgp.permission_id AND p.active = 1
		 WHERE g.id = @p1`
	rows, err := r.db.QueryContext(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		group *domain.PermissionGroup
		found bool
	)
	for rows.Next() {
		found = true
		var (
			gID                          int64
			gCode, gName, gDescription   string
			gActive                      bool
			gCreatedAt                   sql.NullTime
			gUpdatedAt                   sql.NullTime
			pID                          sql.NullInt64
			pScope, pCode, pName, pDescr sql.NullString
			pActive                      sql.NullBool
			pCreated                     sql.NullTime
			pUpdated                     sql.NullTime
		)
		if err := rows.Scan(
			&gID, &gCode, &gName, &gDescription, &gActive,
			&gCreatedAt, &gUpdatedAt,
			&pID, &pScope, &pCode, &pName, &pDescr, &pActive,
			&pCreated, &pUpdated,
		); err != nil {
			return nil, err
		}
		if group == nil {
			group = &domain.PermissionGroup{
				ID:          uint32(gID),
				Code:        gCode,
				Name:        gName,
				Description: gDescription,
				Active:      gActive,
				Permissions: []domain.Permission{},
			}
			if gCreatedAt.Valid {
				group.CreatedAt = gCreatedAt.Time
			}
			if gUpdatedAt.Valid {
				t := gUpdatedAt.Time
				group.UpdatedAt = &t
			}
		}
		if pID.Valid {
			perm := domain.Permission{
				ID:          uint32(pID.Int64),
				Scope:       pScope.String,
				Code:        pCode.String,
				Name:        pName.String,
				Description: pDescr.String,
				Active:      pActive.Bool,
			}
			if pCreated.Valid {
				perm.CreatedAt = pCreated.Time
			}
			if pUpdated.Valid {
				t := pUpdated.Time
				perm.UpdatedAt = &t
			}
			group.Permissions = append(group.Permissions, perm)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrPermGroupNotFound
	}
	return group, nil
}

// FindGroupByCode espejo de FindGroupByID pero lookup por code estable
// ("student_permissions", "asesor_permissions", etc.). El SELECT es
// identico salvo el WHERE. Usado por el flujo de auto-registro publico
// de estudiantes para resolver el id del grupo sin hard-codear.
func (r *PermissionRepo) FindGroupByCode(ctx context.Context, code string) (*domain.PermissionGroup, error) {
	const q = `
		SELECT g.id, g.code, g.name, ISNULL(g.description, ''), g.active,
		       g.created_at, g.updated_at,
		       p.id, p.scope, p.code, p.name, ISNULL(p.description, ''), p.active,
		       p.created_at, p.updated_at
		  FROM permission_group g
		  LEFT JOIN permission_group_permission pgp ON pgp.permission_group_id = g.id
		  LEFT JOIN permission p                    ON p.id = pgp.permission_id AND p.active = 1
		 WHERE g.code = @p1`
	rows, err := r.db.QueryContext(ctx, q, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		group *domain.PermissionGroup
		found bool
	)
	for rows.Next() {
		found = true
		var (
			gID                          int64
			gCode, gName, gDescription   string
			gActive                      bool
			gCreatedAt                   sql.NullTime
			gUpdatedAt                   sql.NullTime
			pID                          sql.NullInt64
			pScope, pCode, pName, pDescr sql.NullString
			pActive                      sql.NullBool
			pCreated                     sql.NullTime
			pUpdated                     sql.NullTime
		)
		if err := rows.Scan(
			&gID, &gCode, &gName, &gDescription, &gActive,
			&gCreatedAt, &gUpdatedAt,
			&pID, &pScope, &pCode, &pName, &pDescr, &pActive,
			&pCreated, &pUpdated,
		); err != nil {
			return nil, err
		}
		if group == nil {
			group = &domain.PermissionGroup{
				ID:          uint32(gID),
				Code:        gCode,
				Name:        gName,
				Description: gDescription,
				Active:      gActive,
				Permissions: []domain.Permission{},
			}
			if gCreatedAt.Valid {
				group.CreatedAt = gCreatedAt.Time
			}
			if gUpdatedAt.Valid {
				t := gUpdatedAt.Time
				group.UpdatedAt = &t
			}
		}
		if pID.Valid {
			perm := domain.Permission{
				ID:          uint32(pID.Int64),
				Scope:       pScope.String,
				Code:        pCode.String,
				Name:        pName.String,
				Description: pDescr.String,
				Active:      pActive.Bool,
			}
			if pCreated.Valid {
				perm.CreatedAt = pCreated.Time
			}
			if pUpdated.Valid {
				t := pUpdated.Time
				perm.UpdatedAt = &t
			}
			group.Permissions = append(group.Permissions, perm)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrPermGroupNotFound
	}
	return group, nil
}

// ListGroups retorna todos los grupos activos con sus permisos.
// Para evitar el N+1 (un SELECT por grupo) hacemos UNA sola query con
// LEFT JOIN y agrupamos en memoria por group.id. La query devuelve hasta
// |grupos| × |permisos por grupo| filas, ordenadas por g.id para que
// el switch de "nuevo grupo" sea barato.
func (r *PermissionRepo) ListGroups(ctx context.Context) ([]domain.PermissionGroup, error) {
	const q = `
		SELECT g.id, g.code, g.name, ISNULL(g.description, ''), g.active,
		       g.created_at, g.updated_at,
		       p.id, p.scope, p.code, p.name, ISNULL(p.description, ''), p.active,
		       p.created_at, p.updated_at
		  FROM permission_group g
		  LEFT JOIN permission_group_permission pgp ON pgp.permission_group_id = g.id
		  LEFT JOIN permission p                    ON p.id = pgp.permission_id AND p.active = 1
		 WHERE g.active = 1
		 ORDER BY g.id, p.scope, p.code`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]domain.PermissionGroup, 0, 8)
	// índice: group_id -> posición en `groups`. Evita búsquedas O(n).
	idx := make(map[uint32]int, 8)

	for rows.Next() {
		var (
			gID                          int64
			gCode, gName, gDescription   string
			gActive                      bool
			gCreatedAt                   sql.NullTime
			gUpdatedAt                   sql.NullTime
			pID                          sql.NullInt64
			pScope, pCode, pName, pDescr sql.NullString
			pActive                      sql.NullBool
			pCreated                     sql.NullTime
			pUpdated                     sql.NullTime
		)
		if err := rows.Scan(
			&gID, &gCode, &gName, &gDescription, &gActive,
			&gCreatedAt, &gUpdatedAt,
			&pID, &pScope, &pCode, &pName, &pDescr, &pActive,
			&pCreated, &pUpdated,
		); err != nil {
			return nil, err
		}
		gid := uint32(gID)
		pos, ok := idx[gid]
		if !ok {
			g := domain.PermissionGroup{
				ID:          gid,
				Code:        gCode,
				Name:        gName,
				Description: gDescription,
				Active:      gActive,
				Permissions: []domain.Permission{},
			}
			if gCreatedAt.Valid {
				g.CreatedAt = gCreatedAt.Time
			}
			if gUpdatedAt.Valid {
				t := gUpdatedAt.Time
				g.UpdatedAt = &t
			}
			groups = append(groups, g)
			pos = len(groups) - 1
			idx[gid] = pos
		}
		if pID.Valid {
			perm := domain.Permission{
				ID:          uint32(pID.Int64),
				Scope:       pScope.String,
				Code:        pCode.String,
				Name:        pName.String,
				Description: pDescr.String,
				Active:      pActive.Bool,
			}
			if pCreated.Valid {
				perm.CreatedAt = pCreated.Time
			}
			if pUpdated.Valid {
				t := pUpdated.Time
				perm.UpdatedAt = &t
			}
			groups[pos].Permissions = append(groups[pos].Permissions, perm)
		}
	}
	return groups, rows.Err()
}

// ListGroupsByUser devuelve los grupos (perfiles de acceso) a los que
// pertenece un usuario (via user_permission_group), con sus permisos. Misma
// forma que ListGroups pero acotado por user_id.
func (r *PermissionRepo) ListGroupsByUser(ctx context.Context, userID domain.UserID) ([]domain.PermissionGroup, error) {
	const q = `
		SELECT g.id, g.code, g.name, ISNULL(g.description, ''), g.active,
		       g.created_at, g.updated_at,
		       p.id, p.scope, p.code, p.name, ISNULL(p.description, ''), p.active,
		       p.created_at, p.updated_at
		  FROM permission_group g
		  JOIN user_permission_group upg ON upg.permission_group_id = g.id
		                                 AND UPPER(CONVERT(NVARCHAR(36), upg.user_id)) = UPPER(@p1)
		  LEFT JOIN permission_group_permission pgp ON pgp.permission_group_id = g.id
		  LEFT JOIN permission p                    ON p.id = pgp.permission_id AND p.active = 1
		 WHERE g.active = 1
		 ORDER BY g.id, p.scope, p.code`
	rows, err := r.db.QueryContext(ctx, q, string(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]domain.PermissionGroup, 0, 4)
	idx := make(map[uint32]int, 4)
	for rows.Next() {
		var (
			gID                          int64
			gCode, gName, gDescription   string
			gActive                      bool
			gCreatedAt                   sql.NullTime
			gUpdatedAt                   sql.NullTime
			pID                          sql.NullInt64
			pScope, pCode, pName, pDescr sql.NullString
			pActive                      sql.NullBool
			pCreated                     sql.NullTime
			pUpdated                     sql.NullTime
		)
		if err := rows.Scan(
			&gID, &gCode, &gName, &gDescription, &gActive,
			&gCreatedAt, &gUpdatedAt,
			&pID, &pScope, &pCode, &pName, &pDescr, &pActive,
			&pCreated, &pUpdated,
		); err != nil {
			return nil, err
		}
		gid := uint32(gID)
		pos, ok := idx[gid]
		if !ok {
			g := domain.PermissionGroup{
				ID:          gid,
				Code:        gCode,
				Name:        gName,
				Description: gDescription,
				Active:      gActive,
				Permissions: []domain.Permission{},
			}
			if gCreatedAt.Valid {
				g.CreatedAt = gCreatedAt.Time
			}
			if gUpdatedAt.Valid {
				t := gUpdatedAt.Time
				g.UpdatedAt = &t
			}
			groups = append(groups, g)
			pos = len(groups) - 1
			idx[gid] = pos
		}
		if pID.Valid {
			perm := domain.Permission{
				ID:          uint32(pID.Int64),
				Scope:       pScope.String,
				Code:        pCode.String,
				Name:        pName.String,
				Description: pDescr.String,
				Active:      pActive.Bool,
			}
			if pCreated.Valid {
				perm.CreatedAt = pCreated.Time
			}
			if pUpdated.Valid {
				t := pUpdated.Time
				perm.UpdatedAt = &t
			}
			groups[pos].Permissions = append(groups[pos].Permissions, perm)
		}
	}
	return groups, rows.Err()
}

// AddPermissionToGroup asocia un permiso a un grupo (idempotente).
// Validamos primero que ambos existan para devolver errores de dominio
// específicos en lugar del FK genérico de SQL Server (547).
func (r *PermissionRepo) AddPermissionToGroup(ctx context.Context, groupID, permissionID uint32) error {
	gExists, err := r.GroupExists(ctx, groupID)
	if err != nil {
		return err
	}
	if !gExists {
		return domain.ErrPermGroupNotFound
	}
	pExists, err := r.PermissionExists(ctx, permissionID)
	if err != nil {
		return err
	}
	if !pExists {
		return domain.ErrPermissionNotFound
	}

	const q = `
		IF NOT EXISTS (
		    SELECT 1 FROM permission_group_permission
		     WHERE permission_group_id = @p1 AND permission_id = @p2
		)
		BEGIN
		    INSERT INTO permission_group_permission (permission_group_id, permission_id)
		    VALUES (@p1, @p2);
		END`
	_, err = r.db.ExecContext(ctx, q, groupID, permissionID)
	return err
}

// RemovePermissionFromGroup es idempotente: si la asociación no existe,
// no falla. Útil para que el cliente pueda llamar "borrar" sin chequear
// previamente.
func (r *PermissionRepo) RemovePermissionFromGroup(ctx context.Context, groupID, permissionID uint32) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM permission_group_permission
		  WHERE permission_group_id = @p1 AND permission_id = @p2`,
		groupID, permissionID)
	return err
}

// ListPermissions devuelve el catálogo completo de permisos activos,
// ordenado por (scope, code) para una UI estable.
func (r *PermissionRepo) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	const q = `
		SELECT id, scope, code, name, ISNULL(description, ''), active, created_at, updated_at
		  FROM permission
		 WHERE active = 1
		 ORDER BY scope, code`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms := make([]domain.Permission, 0, 32)
	for rows.Next() {
		var (
			id        int64
			scope     string
			code      string
			name      string
			descr     string
			active    bool
			createdAt sql.NullTime
			updatedAt sql.NullTime
		)
		if err := rows.Scan(&id, &scope, &code, &name, &descr, &active, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p := domain.Permission{
			ID:          uint32(id),
			Scope:       scope,
			Code:        code,
			Name:        name,
			Description: descr,
			Active:      active,
		}
		if createdAt.Valid {
			p.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			t := updatedAt.Time
			p.UpdatedAt = &t
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func (r *PermissionRepo) PermissionExists(ctx context.Context, permissionID uint32) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS (SELECT 1 FROM permission WHERE id = @p1 AND active = 1) THEN 1 ELSE 0 END`,
		permissionID).Scan(&exists)
	return exists == 1, err
}

func (r *PermissionRepo) GroupHasUsers(ctx context.Context, groupID uint32) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS (SELECT 1 FROM user_permission_group WHERE permission_group_id = @p1) THEN 1 ELSE 0 END`,
		groupID).Scan(&exists)
	return exists == 1, err
}

// ListUsersInGroup devuelve los users asignados al grupo, con paginación
// y búsqueda libre que matchea email, first_name, last_name o document_number.
func (r *PermissionRepo) ListUsersInGroup(ctx context.Context, in ports.ListUsersInGroupInput) ([]domain.User, uint32, error) {
	if in.Limit == 0 || in.Limit > 1000 {
		in.Limit = 100
	}
	where := "WHERE upg.permission_group_id = @p1"
	args := []any{in.GroupID}
	idx := 2
	if in.ActiveOnly {
		where += " AND u.active = 1"
	}
	if in.Search != "" {
		where += fmt.Sprintf(" AND (u.email LIKE @p%d OR u.first_name LIKE @p%d OR u.last_name LIKE @p%d OR u.document_number LIKE @p%d)", idx, idx, idx, idx)
		args = append(args, "%"+in.Search+"%")
		idx++
	}

	var total uint32
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM user_permission_group upg JOIN users u ON u.id = upg.user_id "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `SELECT CONVERT(NVARCHAR(36), u.id),
		u.email,
		u.password_hash,
		ISNULL(u.first_name, ''),
		ISNULL(u.last_name, ''),
		ISNULL(u.document_number, ''),
		ISNULL(CONVERT(NVARCHAR(36), u.school_id), ''),
		u.active,
		u.last_access_at,
		u.created_at,
		u.updated_at,
		ISNULL(u.hubspot_record_id, ''),
		u.is_superadmin,
		u.must_change_password,
		ISNULL(u.phone, ''),
		u.int_id,
		ISNULL((
			SELECT TOP 1 CASE pg2.code
				WHEN 'admin_permissions' THEN 1
				WHEN 'asesor_permissions' THEN 2
				WHEN 'coordinador_permissions' THEN 3
				WHEN 'student_permissions' THEN 4
				ELSE 0 END
			  FROM user_permission_group upg2
			  JOIN permission_group pg2 ON pg2.id = upg2.permission_group_id
			 WHERE upg2.user_id = u.id
			   AND pg2.active = 1
			   AND pg2.code IN ('admin_permissions','asesor_permissions','coordinador_permissions','student_permissions')
			 ORDER BY CASE pg2.code
				WHEN 'admin_permissions' THEN 1
				WHEN 'asesor_permissions' THEN 2
				WHEN 'coordinador_permissions' THEN 3
				WHEN 'student_permissions' THEN 4
				ELSE 99 END ASC
		), 0) AS user_type
	        FROM user_permission_group upg
	        JOIN users u ON u.id = upg.user_id
	        ` + where + `
	       ORDER BY u.last_name ASC, u.first_name ASC, u.id ASC
	      OFFSET @p` + strconv.Itoa(idx) + ` ROWS FETCH NEXT @p` + strconv.Itoa(idx+1) + ` ROWS ONLY`
	args = append(args, in.Offset, in.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

// mapPermGroupDuplicate traduce una violación de uk_permission_group_code
// (codes 2627 / 2601) a domain.ErrPermGroupCodeTaken. Cualquier otro
// error se propaga sin tocar.
func mapPermGroupDuplicate(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && (msErr.Number == 2627 || msErr.Number == 2601) {
		msg := strings.ToLower(msErr.Message)
		if strings.Contains(msg, "uk_permission_group_code") || strings.Contains(msg, "permission_group") {
			return domain.ErrPermGroupCodeTaken
		}
	}
	return err
}

// mapPermFK traduce errores de FK (547) sobre permission_group_permission
// a un error de dominio claro. El caso más probable es un permission_id
// inexistente al asociar permisos en CreateGroup.
func mapPermFK(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && msErr.Number == 547 {
		return domain.ErrPermissionNotFound
	}
	return err
}
