package mssqladapter

import (
	"context"
	"database/sql"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// StudentClassifier resuelve "¿el user X loguea via OTP?" consultando si
// pertenece al permission_group con code 'student_permissions'.
type StudentClassifier struct {
	db *sql.DB
}

var _ ports.StudentClassifier = (*StudentClassifier)(nil)

func NewStudentClassifier(db *sql.DB) *StudentClassifier {
	return &StudentClassifier{db: db}
}

func (c *StudentClassifier) IsStudent(ctx context.Context, userID domain.UserID) (bool, error) {
	const q = `
		SELECT 1
		  FROM user_permission_group upg
		  JOIN permission_group pg ON pg.id = upg.permission_group_id
		 WHERE upg.user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND pg.code = 'student_permissions'`
	var x int
	err := c.db.QueryRowContext(ctx, q, string(userID)).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
