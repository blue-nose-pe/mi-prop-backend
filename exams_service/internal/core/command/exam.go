package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// prefijo por exam_type. Si se agregan nuevos tipos en la DB hay que mapearlos
// aca para que el autogen produzca codigos legibles. Si no hay match, se usa
// "EXAM" como fallback.
var examCodePrefix = map[string]string{
	"vocacional": "VOCC",
	"simulacro":  "SIME",
	"habitos":    "ESTI",
}

// versionSuffix detecta "...-V{n}" al final del codigo, para que al clonar
// V1 -> V2 -> V3 se reemplace el sufijo en vez de acumularlo.
var versionSuffix = regexp.MustCompile(`-[vV]\d+$`)

func examCodePrefixFor(typeCode string) string {
	if p, ok := examCodePrefix[strings.ToLower(strings.TrimSpace(typeCode))]; ok {
		return p
	}
	return "EXAM"
}

// generateExamCode arma "{PREFIX}-{YYYY}-V{version}" y, si ya existe, le
// agrega un sufijo hex aleatorio para asegurar unicidad. Se intenta a lo
// sumo 5 veces; un error de la DB en el penultimo intento se propaga.
func generateExamCode(ctx context.Context, repo ports.ExamRepository, typeCode string, version int32) (string, error) {
	base := fmt.Sprintf("%s-%d-V%d", examCodePrefixFor(typeCode), time.Now().UTC().Year(), version)
	if exists, err := repo.ExistsByCode(ctx, base); err != nil {
		return "", err
	} else if !exists {
		return base, nil
	}
	for i := 0; i < 5; i++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s-%s", base, strings.ToUpper(hex.EncodeToString(b[:])))
		if exists, err := repo.ExistsByCode(ctx, candidate); err != nil {
			return "", err
		} else if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not allocate a unique exam code after retries")
}

// nextCloneCode toma el codigo del exam padre y reemplaza/agrega el sufijo
// "-V{n}" con la nueva version del clon. Si el resultado ya esta en uso
// (raro pero posible: alguien creo un exam suelto con ese codigo), cae al
// autogen estandar con sufijo aleatorio.
func nextCloneCode(ctx context.Context, repo ports.ExamRepository, parentCode, parentTypeCode string, newVersion int32) (string, error) {
	base := strings.TrimSpace(parentCode)
	base = versionSuffix.ReplaceAllString(base, "")
	if base == "" {
		base = fmt.Sprintf("%s-%d", examCodePrefixFor(parentTypeCode), time.Now().UTC().Year())
	}
	candidate := fmt.Sprintf("%s-V%d", base, newVersion)
	if exists, err := repo.ExistsByCode(ctx, candidate); err != nil {
		return "", err
	} else if !exists {
		return candidate, nil
	}
	// Colision: dejar que generateExamCode maneje el sufijo aleatorio.
	return generateExamCode(ctx, repo, parentTypeCode, newVersion)
}

// ExamHandler implementa ports.ExamCommands. Mutaciones sobre exam.
//
// Reglas de negocio:
//   - Update solo aplica a exámenes NO publicados (preserva inmutabilidad
//     post-publicación). Para "editar" un exam publicado, hay que Clone().
//   - Publish requiere que el exam tenga al menos una pregunta (lo verifica
//     a través de exam_question repo).
//   - Clone copia el exam y sus preguntas → genera versión nueva con
//     parent_exam_id apuntando al original. Histórico preservado.
type ExamHandler struct {
	types         ports.ExamTypeRepository
	exams         ports.ExamRepository
	examQuestions ports.ExamQuestionRepository
}

var _ ports.ExamCommands = (*ExamHandler)(nil)

func NewExamHandler(
	types ports.ExamTypeRepository,
	exams ports.ExamRepository,
	examQuestions ports.ExamQuestionRepository,
) *ExamHandler {
	return &ExamHandler{types: types, exams: exams, examQuestions: examQuestions}
}

func (h *ExamHandler) Create(ctx context.Context, in ports.CreateExamInput) (*domain.Exam, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, domain.ErrEmptyExamName
	}
	if !in.StartAt.Before(in.EndAt) {
		return nil, domain.ErrInvalidDateRange
	}

	t, err := h.types.FindByCode(ctx, strings.ToLower(strings.TrimSpace(in.ExamTypeCode)))
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		code, err = generateExamCode(ctx, h.exams, t.Code, 1)
		if err != nil {
			return nil, err
		}
	} else {
		if exists, err := h.exams.ExistsByCode(ctx, code); err != nil {
			return nil, err
		} else if exists {
			return nil, domain.ErrExamCodeTaken
		}
	}

	// Reglas de puntaje por defecto: correcta<=0 se normaliza a 1 para que un
	// examen nuevo nunca quede con preguntas que no puntuan por accidente.
	defPts := in.DefaultPoints
	if defPts <= 0 {
		defPts = 1
	}
	e := &domain.Exam{
		ExamTypeID:             t.ID,
		SchoolID:               in.SchoolID,
		Code:                   code,
		Name:                   strings.TrimSpace(in.Name),
		StartAt:                in.StartAt,
		EndAt:                  in.EndAt,
		MaxParticipants:        in.MaxParticipants,
		DefaultPoints:          defPts,
		DefaultPointsIncorrect: in.DefaultPointsIncorrect,
		DefaultPointsBlank:     in.DefaultPointsBlank,
		DurationMinutes:        in.DurationMinutes, // B5: nil = default por tipo
		Version:                1,
		Active:                 true,
		Published:              false,
	}
	id, err := h.exams.Save(ctx, e)
	if err != nil {
		return nil, err
	}
	// Refetch para hidratar timestamps asignados por la DB (created_at,
	// updated_at). Sin esto el response inmediato traia zero-value de Go.
	return h.exams.FindByID(ctx, id)
}

func (h *ExamHandler) Update(ctx context.Context, in ports.UpdateExamInput) (*domain.Exam, error) {
	e, err := h.exams.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if e.Published {
		return nil, domain.ErrCannotEditPublished
	}
	if v := strings.TrimSpace(in.Name); v != "" {
		e.Name = v
	}
	if v := strings.TrimSpace(in.Code); v != "" && v != e.Code {
		exists, err := h.exams.ExistsByCode(ctx, v)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.ErrExamCodeTaken
		}
		e.Code = v
	}
	if !in.StartAt.IsZero() {
		e.StartAt = in.StartAt
	}
	if !in.EndAt.IsZero() {
		e.EndAt = in.EndAt
	}
	if in.MaxParticipants >= 0 {
		e.MaxParticipants = in.MaxParticipants
	}
	if in.DefaultPoints != nil {
		e.DefaultPoints = *in.DefaultPoints
	}
	if in.DefaultPointsIncorrect != nil {
		e.DefaultPointsIncorrect = *in.DefaultPointsIncorrect
	}
	if in.DefaultPointsBlank != nil {
		e.DefaultPointsBlank = *in.DefaultPointsBlank
	}
	if in.DurationMinutes != nil { // B5: timer configurable (nil = no tocar)
		if *in.DurationMinutes < 0 {
			e.DurationMinutes = nil // negativo = resetear al default por tipo
		} else {
			e.DurationMinutes = in.DurationMinutes
		}
	}
	if !e.StartAt.Before(e.EndAt) {
		return nil, domain.ErrInvalidDateRange
	}
	if err := h.exams.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

func (h *ExamHandler) Publish(ctx context.Context, id domain.ExamID) error {
	if _, err := h.exams.FindByID(ctx, id); err != nil {
		return err
	}
	qs, err := h.examQuestions.List(ctx, id)
	if err != nil {
		return err
	}
	if len(qs) == 0 {
		return domain.ErrEmptyExamName // reuse — un publicar sin preguntas es inválido
	}
	if err := h.exams.SetPublished(ctx, id, true); err != nil {
		return err
	}
	// Publicar tambien REACTIVA: un examen pausado (active=false) que se
	// publica debe quedar VIVO (active=true + published=true). Antes Publish
	// solo seteaba published=true, asi que publicar un examen "paused" lo
	// dejaba igual (paused) — el boton "play" no hacia nada visible.
	return h.exams.SetActive(ctx, id, true)
}

func (h *ExamHandler) Deactivate(ctx context.Context, id domain.ExamID) error {
	return h.exams.SetActive(ctx, id, false)
}

func (h *ExamHandler) Reactivate(ctx context.Context, id domain.ExamID) error {
	return h.exams.SetActive(ctx, id, true)
}

// Clone genera una nueva versión del exam (parent_exam_id = id), copiando
// todas sus exam_question. La version del clon es max(version) de toda la
// familia + 1, asi cada clon (incluyendo clones del mismo src) obtiene una
// version unica monotona. Permite "editar" un exam publicado sin perder los
// attempts antiguos.
func (h *ExamHandler) Clone(ctx context.Context, id domain.ExamID) (*domain.Exam, error) {
	src, err := h.exams.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	maxVer, err := h.exams.MaxVersionInFamily(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	newVersion := maxVer + 1

	t, err := h.types.FindByID(ctx, src.ExamTypeID)
	if err != nil {
		return nil, err
	}
	cloneCode, err := nextCloneCode(ctx, h.exams, src.Code, t.Code, newVersion)
	if err != nil {
		return nil, err
	}

	clone := &domain.Exam{
		ExamTypeID:      src.ExamTypeID,
		SchoolID:        src.SchoolID,
		ParentExamID:    src.ID,
		Version:         newVersion,
		Code:            cloneCode,
		Name:            src.Name,
		StartAt:         src.StartAt,
		EndAt:           src.EndAt,
		MaxParticipants: src.MaxParticipants,
		// Fix (audit 2026-06-18): copiar los puntajes default del examen padre. Sin
		// esto el clon nacía con DefaultPoints=0 y las preguntas NUEVAS que se le
		// agregaban (las clonadas traen sus puntos propios) puntuaban 0.
		DefaultPoints:          src.DefaultPoints,
		DefaultPointsIncorrect: src.DefaultPointsIncorrect,
		DefaultPointsBlank:     src.DefaultPointsBlank,
		DurationMinutes:        src.DurationMinutes, // B5: el clon hereda el timer del padre
		Active:                 true,
		Published:              false, // la nueva versión nace en draft
	}
	newID, err := h.exams.Save(ctx, clone)
	if err != nil {
		return nil, err
	}
	if err := h.examQuestions.CloneInto(ctx, src.ID, newID); err != nil {
		return nil, err
	}
	return h.exams.FindByID(ctx, newID)
}
