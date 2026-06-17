package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type LeadRepo struct {
	db *sql.DB
}

var _ ports.LeadRepository = (*LeadRepo)(nil)

func NewLeadRepo(db *sql.DB) *LeadRepo { return &LeadRepo{db: db} }

// Create persiste un lead nuevo. Modelo APPEND: cada inscripcion es una fila
// (sin upsert por dni/email — fiel a prod). La BD genera el UUID via DEFAULT
// NEWID() y lo recuperamos con OUTPUT INSERTED.id. graduation_year 0 => NULL.
//
// Estado de acceso: si el lead trae key_code (hubo una LAN ACTIVA → la landing
// auto-envia el magic-link al registrar), lo marcamos como ENVIADO en la
// creacion (acceso_enviado_at = ahora, acceso_key_code = la LAN). Si key_code
// es vacio (sin campaña activa → edge case), queda PENDIENTE y el staff se lo
// envia desde el panel. Asi el panel refleja el estado real, no "Pendiente"
// para alguien que ya recibio su acceso.
func (r *LeadRepo) Create(ctx context.Context, l *domain.Lead) (domain.LeadID, error) {
	const q = `
		INSERT INTO landing_lead (first_name, last_name, dni, phone, email,
		                          graduation_year, school_text, origen, key_code,
		                          terms_accepted, data_processing, hubspot_record_id,
		                          acceso_enviado_at, acceso_key_code)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, @p2, NULLIF(@p3, ''), NULLIF(@p4, ''), @p5,
		        NULLIF(@p6, 0), NULLIF(@p7, ''), @p8, NULLIF(@p9, ''),
		        @p10, @p11, NULLIF(@p12, ''),
		        IIF(@p9 = '', NULL, SYSUTCDATETIME()), NULLIF(@p9, ''))`
	origen := l.Origen
	if origen == "" {
		origen = "landing Simulacro"
	}
	var id string
	err := r.db.QueryRowContext(ctx, q,
		l.FirstName, l.LastName, l.DNI, l.Phone, l.Email,
		l.GraduationYear, l.SchoolText, origen, l.KeyCode,
		l.TermsAccepted, l.DataProcessing, l.HubspotRecordID,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.LeadID(id), nil
}

// List devuelve leads paginados (mas reciente primero) con su total sin
// paginar. Filtra por texto (nombre/dni/correo, case-insensitive) y/o por
// key_code de campaña.
func (r *LeadRepo) List(ctx context.Context, in ports.ListLeadsInput) ([]domain.Lead, uint32, error) {
	if in.Limit == 0 || in.Limit > 1000 {
		in.Limit = 100
	}
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if in.Search != "" {
		p := "@p" + strconv.Itoa(idx)
		where += " AND ((first_name + ' ' + last_name) COLLATE Latin1_General_CI_AI LIKE " + p +
			" OR ISNULL(dni,'') LIKE " + p + " OR email COLLATE Latin1_General_CI_AI LIKE " + p + ")"
		args = append(args, "%"+in.Search+"%")
		idx++
	}
	if in.KeyCode != "" {
		where += " AND key_code = @p" + strconv.Itoa(idx)
		args = append(args, in.KeyCode)
		idx++
	}

	var total uint32
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM landing_lead "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `SELECT ` + leadCols + `
	        FROM landing_lead ` + where + `
	       ORDER BY created_at DESC
	      OFFSET @p` + strconv.Itoa(idx) + ` ROWS FETCH NEXT @p` + strconv.Itoa(idx+1) + ` ROWS ONLY`
	args = append(args, in.Offset, in.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.Lead, 0)
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *l)
	}
	return out, total, rows.Err()
}

func (r *LeadRepo) FindByID(ctx context.Context, id domain.LeadID) (*domain.Lead, error) {
	const q = `SELECT ` + leadCols + ` FROM landing_lead WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	row := r.db.QueryRowContext(ctx, q, string(id))
	l, err := scanLead(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrLeadNotFound
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

// MarkAccessSent marca que al lead se le envio el correo de acceso (magic-link)
// con la key indicada. El caller (panel masivo) saltea los ya enviados; aqui
// siempre setea, asi un reenvio actualiza timestamp/key.
func (r *LeadRepo) MarkAccessSent(ctx context.Context, id domain.LeadID, keyCode string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE landing_lead
		    SET acceso_enviado_at = SYSUTCDATETIME(), acceso_key_code = NULLIF(@p1, '')
		  WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		keyCode, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrLeadNotFound
	}
	return nil
}

// ListByIDs devuelve los leads cuyos ids estan en la lista. Lo usa el envio
// masivo del panel: el front manda los ids seleccionados y el gateway resuelve
// sus datos (email/nombre/dni/phone) para mandarles el acceso. Cap defensivo
// en el caller (no pasar miles de ids).
func (r *LeadRepo) ListByIDs(ctx context.Context, ids []domain.LeadID) ([]domain.Lead, error) {
	if len(ids) == 0 {
		return []domain.Lead{}, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "CONVERT(UNIQUEIDENTIFIER, @p" + strconv.Itoa(i+1) + ")"
		args[i] = string(id)
	}
	q := `SELECT ` + leadCols + ` FROM landing_lead WHERE id IN (` + strings.Join(ph, ",") + `)`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Lead, 0, len(ids))
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *LeadRepo) SetHubspotRecordID(ctx context.Context, id domain.LeadID, recordID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE landing_lead SET hubspot_record_id = NULLIF(@p1, '')
		  WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		recordID, string(id))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrLeadNotFound
	}
	return nil
}

const leadCols = `CONVERT(NVARCHAR(36), id),
	first_name, last_name, ISNULL(dni, ''), ISNULL(phone, ''), email,
	ISNULL(graduation_year, 0), ISNULL(school_text, ''), origen,
	ISNULL(key_code, ''), terms_accepted, data_processing,
	ISNULL(hubspot_record_id, ''), created_at,
	acceso_enviado_at, ISNULL(acceso_key_code, '')`

// scanRow abstrae *sql.Row y *sql.Rows (ambos exponen Scan).
type scanRow interface {
	Scan(dest ...any) error
}

func scanLead(row scanRow) (*domain.Lead, error) {
	var (
		l        domain.Lead
		idStr    string
		accesoAt sql.NullTime
	)
	if err := row.Scan(
		&idStr, &l.FirstName, &l.LastName, &l.DNI, &l.Phone, &l.Email,
		&l.GraduationYear, &l.SchoolText, &l.Origen, &l.KeyCode,
		&l.TermsAccepted, &l.DataProcessing, &l.HubspotRecordID, &l.CreatedAt,
		&accesoAt, &l.AccesoKeyCode,
	); err != nil {
		return nil, err
	}
	if accesoAt.Valid {
		l.AccesoEnviadoAt = &accesoAt.Time
	}
	l.ID = domain.LeadID(idStr)
	return &l, nil
}
