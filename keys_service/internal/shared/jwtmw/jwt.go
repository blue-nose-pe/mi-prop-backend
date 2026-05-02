// Package jwtmw centraliza la emisión y validación de JWT para todos
// los microservicios. Mi Propósito 2.0 usa:
//
//   - access token (15 min) firmado HS256
//   - refresh token (7 días) firmado HS256, además persistido server-side
//     en tabla refresh_token (db_users) para permitir revocación
//
// Header esperado: `Authorization: Bearer <token>`.
//
// Claims canónicos:
//
//	sub          string  user.id (UUID)
//	email        string
//	roles        []string  ej ["admin"], ["asesor"], ["estudiante"]
//	permissions  []string  códigos granulares ej ["users.view","colegios.create"]
//	school_id    string    UUID de school si user pertenece a uno
//	type         string    "access" | "refresh"
//	jti          string    id único del token (para revocar refresh)
//	iat, exp     int64     standard claims
package jwtmw

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims son los campos que viajan dentro del JWT.
type Claims struct {
	Email       string   `json:"email,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	SchoolID    string   `json:"school_id,omitempty"`
	Type        string   `json:"type"` // "access" | "refresh"
	jwt.RegisteredClaims
}

// Signer emite tokens. La instancia se construye una vez al arrancar
// el servicio con la clave secreta cargada desde Key Vault.
type Signer struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// SignerConfig agrupa la config del signer.
type SignerConfig struct {
	Secret     []byte        // mínimo 32 bytes para HS256
	Issuer     string        // ej "miproposito.users"
	AccessTTL  time.Duration // default 15m
	RefreshTTL time.Duration // default 7*24h
}

// NewSigner construye un Signer. Falla si Secret está vacío.
func NewSigner(cfg SignerConfig) (*Signer, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("jwtmw: secret too short (need >= 32 bytes for HS256)")
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "miproposito"
	}
	return &Signer{
		secret:     cfg.Secret,
		issuer:     cfg.Issuer,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
	}, nil
}

// IssueParams son los datos del usuario para emitir un par access+refresh.
type IssueParams struct {
	UserID      string
	Email       string
	Roles       []string
	Permissions []string
	SchoolID    string
}

// IssuePair emite (access, refresh) y devuelve también el jti del refresh
// para que el servicio lo persista en la tabla refresh_token.
type Pair struct {
	AccessToken  string
	RefreshToken string
	RefreshJTI   string
	AccessExp    time.Time
	RefreshExp   time.Time
}

// IssuePair genera un par access+refresh para el usuario.
func (s *Signer) IssuePair(p IssueParams) (Pair, error) {
	now := time.Now().UTC()
	accessExp := now.Add(s.accessTTL)
	refreshExp := now.Add(s.refreshTTL)
	refreshJTI := uuid.NewString()

	access, err := s.sign(Claims{
		Email:       p.Email,
		Roles:       p.Roles,
		Permissions: p.Permissions,
		SchoolID:    p.SchoolID,
		Type:        "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   p.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExp),
			ID:        uuid.NewString(),
		},
	})
	if err != nil {
		return Pair{}, err
	}
	refresh, err := s.sign(Claims{
		Type: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   p.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			ID:        refreshJTI,
		},
	})
	if err != nil {
		return Pair{}, err
	}
	return Pair{
		AccessToken:  access,
		RefreshToken: refresh,
		RefreshJTI:   refreshJTI,
		AccessExp:    accessExp,
		RefreshExp:   refreshExp,
	}, nil
}

func (s *Signer) sign(c Claims) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString(s.secret)
}

// Verifier valida tokens entrantes.
type Verifier struct {
	secret []byte
	issuer string
}

// NewVerifier construye un Verifier (la secret debe coincidir con la del Signer).
func NewVerifier(secret []byte, issuer string) *Verifier {
	return &Verifier{secret: secret, issuer: issuer}
}

// Verify parsea un token y devuelve sus claims si es válido (firma + exp + issuer).
// El caller debe verificar adicionalmente Claims.Type == "access" o "refresh"
// según el caso de uso.
func (v *Verifier) Verify(tokenStr string) (*Claims, error) {
	c := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwtmw: unexpected signing method %v", t.Header["alg"])
		}
		return v.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(v.issuer))
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("jwtmw: invalid token")
	}
	return c, nil
}
