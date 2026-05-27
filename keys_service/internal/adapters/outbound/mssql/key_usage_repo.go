package mssqladapter

import (
	"context"
	"database/sql"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
)

type KeyUsageRepo struct {
	db *sql.DB
}

var _ ports.KeyUsageRepository = (*KeyUsageRepo)(nil)

func NewKeyUsageRepo(db *sql.DB) *KeyUsageRepo { return &KeyUsageRepo{db: db} }

func (r *KeyUsageRepo) Save(ctx context.Context, u *domain.KeyUsage) error {
	const q = `
		INSERT INTO key_usage (key_id, user_id, attempt_id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)))`
	_, err := r.db.ExecContext(ctx, q,
		string(u.KeyID), string(u.UserID), u.AttemptID)
	return err
}

func (r *KeyUsageRepo) ExistsByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (bool, error) {
	const q = `
		SELECT CASE WHEN EXISTS (
		  SELECT 1 FROM key_usage
		   WHERE key_id  = CONVERT(UNIQUEIDENTIFIER, @p1)
		     AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		) THEN 1 ELSE 0 END`
	var v int
	if err := r.db.QueryRowContext(ctx, q, string(keyID), string(userID)).Scan(&v); err != nil {
		return false, err
	}
	return v == 1, nil
}
