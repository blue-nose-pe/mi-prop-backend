package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type RefreshTokenRepo struct {
	db *sql.DB
}

var _ ports.RefreshTokenRepository = (*RefreshTokenRepo)(nil)

func NewRefreshTokenRepo(db *sql.DB) *RefreshTokenRepo {
	return &RefreshTokenRepo{db: db}
}

func (r *RefreshTokenRepo) Save(ctx context.Context, rec *ports.RefreshTokenRecord) error {
	const q = `
		INSERT INTO refresh_token (jti, user_id, issued_at, expires_at, ip, user_agent)
		VALUES (@p1, CONVERT(UNIQUEIDENTIFIER, @p2), @p3, @p4, NULLIF(@p5, ''), NULLIF(@p6, ''))`
	_, err := r.db.ExecContext(ctx, q,
		rec.JTI, string(rec.UserID), rec.IssuedAt, rec.ExpiresAt, rec.IP, rec.UserAgent)
	return err
}

func (r *RefreshTokenRepo) FindByJTI(ctx context.Context, jti string) (*ports.RefreshTokenRecord, error) {
	const q = `
		SELECT jti,
		       CONVERT(NVARCHAR(36), user_id),
		       issued_at, expires_at, revoked_at,
		       ISNULL(replaced_by, ''),
		       ISNULL(ip, ''), ISNULL(user_agent, '')
		  FROM refresh_token
		 WHERE jti = @p1`

	var (
		rec       ports.RefreshTokenRecord
		userIDStr string
		revoked   sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, q, jti).Scan(
		&rec.JTI, &userIDStr, &rec.IssuedAt, &rec.ExpiresAt,
		&revoked, &rec.ReplacedBy, &rec.IP, &rec.UserAgent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, err
	}
	rec.UserID = domain.UserID(userIDStr)
	if revoked.Valid {
		t := revoked.Time
		rec.RevokedAt = &t
	}
	return &rec, nil
}

// Revoke marca el token como revocado y opcionalmente registra qué jti
// lo reemplazó (rotación). replacedBy = "" para revocación pura (Logout).
func (r *RefreshTokenRepo) Revoke(ctx context.Context, jti string, replacedBy string) error {
	const q = `
		UPDATE refresh_token
		   SET revoked_at  = SYSUTCDATETIME(),
		       replaced_by = NULLIF(@p2, '')
		 WHERE jti = @p1
		   AND revoked_at IS NULL`
	_, err := r.db.ExecContext(ctx, q, jti, replacedBy)
	return err
}

func (r *RefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID domain.UserID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_token
		    SET revoked_at = SYSUTCDATETIME()
		  WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND revoked_at IS NULL`,
		string(userID))
	return err
}
