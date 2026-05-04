package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type OTPRepo struct {
	db *sql.DB
}

var _ ports.OTPRepository = (*OTPRepo)(nil)

func NewOTPRepo(db *sql.DB) *OTPRepo { return &OTPRepo{db: db} }

func (r *OTPRepo) Save(ctx context.Context, t *domain.OTPToken) error {
	const q = `
		INSERT INTO otp_token (id, user_id, code_hash, issued_at, expires_at, attempts, max_attempts, ip)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        @p3, @p4, @p5, @p6, @p7, NULLIF(@p8, ''))`
	_, err := r.db.ExecContext(ctx, q,
		t.ID, string(t.UserID), t.CodeHash, t.IssuedAt, t.ExpiresAt,
		t.Attempts, t.MaxAttempts, t.IP)
	return err
}

func (r *OTPRepo) FindActiveByUser(ctx context.Context, userID domain.UserID) (*domain.OTPToken, error) {
	const q = `
		SELECT TOP 1
		       CONVERT(NVARCHAR(36), id),
		       CONVERT(NVARCHAR(36), user_id),
		       code_hash, issued_at, expires_at, consumed_at,
		       attempts, max_attempts, ISNULL(ip, '')
		  FROM otp_token
		 WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND consumed_at IS NULL
		   AND expires_at > SYSUTCDATETIME()
		 ORDER BY issued_at DESC`

	var (
		t          domain.OTPToken
		idStr      string
		userIDStr  string
		consumedAt sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, q, string(userID)).Scan(
		&idStr, &userIDStr, &t.CodeHash, &t.IssuedAt, &t.ExpiresAt,
		&consumedAt, &t.Attempts, &t.MaxAttempts, &t.IP,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrOTPInvalid
	}
	if err != nil {
		return nil, err
	}
	t.ID = idStr
	t.UserID = domain.UserID(userIDStr)
	if consumedAt.Valid {
		ts := consumedAt.Time
		t.ConsumedAt = &ts
	}
	return &t, nil
}

func (r *OTPRepo) Consume(ctx context.Context, id string) error {
	const q = `
		UPDATE otp_token
		   SET consumed_at = SYSUTCDATETIME()
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND consumed_at IS NULL`
	_, err := r.db.ExecContext(ctx, q, id)
	return err
}

func (r *OTPRepo) IncrementAttempts(ctx context.Context, id string) error {
	// Si attempts+1 >= max_attempts, también consume el OTP (defensa anti-brute-force).
	const q = `
		UPDATE otp_token
		   SET attempts = attempts + 1,
		       consumed_at = CASE
		           WHEN attempts + 1 >= max_attempts THEN SYSUTCDATETIME()
		           ELSE consumed_at
		       END
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	_, err := r.db.ExecContext(ctx, q, id)
	return err
}

func (r *OTPRepo) InvalidateAllForUser(ctx context.Context, userID domain.UserID) error {
	const q = `
		UPDATE otp_token
		   SET consumed_at = SYSUTCDATETIME()
		 WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND consumed_at IS NULL`
	_, err := r.db.ExecContext(ctx, q, string(userID))
	return err
}
