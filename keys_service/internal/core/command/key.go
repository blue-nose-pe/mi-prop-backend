// Package command — write side de keys_service.
package command

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
)

// KeyHandler implementa ports.KeyCommands.
//
// Reglas:
//   - Mode school requiere school_id no vacío.
//   - Si Code llega vacío, generamos uno random (8 chars).
//   - Validate solo lee + chequea ventana/aforo, NO muta.
//   - IncrementUsage es atómico (repo.IncrementUses) y agrega un row
//     en key_usage para auditar.
type KeyHandler struct {
	keys      ports.KeyRepository
	keyUsages ports.KeyUsageRepository
}

var _ ports.KeyCommands = (*KeyHandler)(nil)

func NewKeyHandler(keys ports.KeyRepository, usages ports.KeyUsageRepository) *KeyHandler {
	return &KeyHandler{keys: keys, keyUsages: usages}
}

func (h *KeyHandler) Generate(ctx context.Context, in ports.GenerateKeyInput) (*domain.Key, error) {
	if !in.Mode.Valid() {
		return nil, domain.ErrInvalidMode
	}
	if in.Mode == domain.ModeSchool && string(in.SchoolID) == "" {
		return nil, domain.ErrSchoolRequired
	}
	if in.ValidFrom != nil && in.ValidTo != nil && !in.ValidFrom.Before(*in.ValidTo) {
		return nil, domain.ErrInvalidDateRange
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		code = autogenCode(in.ExamTypeID)
	}

	k := &domain.Key{
		Code:         code,
		ExamTypeID:   in.ExamTypeID,
		SchoolID:     in.SchoolID,
		AsesorUserID: in.AsesorUserID,
		Mode:         in.Mode,
		Grade:        strings.TrimSpace(in.Grade),
		Section:      strings.TrimSpace(in.Section),
		ValidFrom:    in.ValidFrom,
		ValidTo:      in.ValidTo,
		MaxUses:      in.MaxUses,
		Active:       true,
	}
	id, err := h.keys.Save(ctx, k)
	if err != nil {
		return nil, err
	}
	return h.keys.FindByID(ctx, id)
}

func (h *KeyHandler) Update(ctx context.Context, in ports.UpdateKeyInput) (*domain.Key, error) {
	k, err := h.keys.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(in.Grade); v != "" {
		k.Grade = v
	}
	if v := strings.TrimSpace(in.Section); v != "" {
		k.Section = v
	}
	if in.ValidFrom != nil {
		k.ValidFrom = in.ValidFrom
	}
	if in.ValidTo != nil {
		k.ValidTo = in.ValidTo
	}
	if in.MaxUses >= 0 {
		k.MaxUses = in.MaxUses
	}
	if k.ValidFrom != nil && k.ValidTo != nil && !k.ValidFrom.Before(*k.ValidTo) {
		return nil, domain.ErrInvalidDateRange
	}
	if err := h.keys.Update(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

func (h *KeyHandler) Deactivate(ctx context.Context, id domain.KeyID) error {
	return h.keys.SetActive(ctx, id, false)
}

func (h *KeyHandler) Validate(ctx context.Context, code string) (*domain.Key, error) {
	k, err := h.keys.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	ok, _ := k.IsUsable(time.Now().UTC())
	if !ok {
		return nil, domain.ErrKeyNotUsable
	}
	return k, nil
}

func (h *KeyHandler) IncrementUsage(ctx context.Context, in ports.IncrementUsageInput) error {
	rows, err := h.keys.IncrementUses(ctx, in.KeyID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrKeyNotUsable
	}
	return h.keyUsages.Save(ctx, &domain.KeyUsage{
		KeyID:     in.KeyID,
		UserID:    in.UserID,
		AttemptID: in.AttemptID,
		UsedAt:    time.Now().UTC(),
	})
}

// randomCode genera un código alfanumérico (base32 sin padding).
func randomCode(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback determinístico extremadamente improbable; en prod
		// el rand.Read jamás falla.
		return strings.Repeat("X", n)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	if len(enc) > n {
		enc = enc[:n]
	}
	return strings.ToUpper(enc)
}

// autogenCode arma un código tipo `VO-XXXXXX`, `SI-XXXXXX`, `ES-XXXXXX`
// según el exam_type_id (1=vocacional, 2=simulacro, 3=hábitos). Para
// tipos desconocidos cae al fallback alfanumérico de 8 chars sin prefijo.
func autogenCode(examTypeID int32) string {
	var prefix string
	switch examTypeID {
	case 1:
		prefix = "VO"
	case 2:
		prefix = "SI"
	case 3:
		prefix = "ES"
	default:
		return randomCode(8)
	}
	return prefix + "-" + randomCode(6)
}
