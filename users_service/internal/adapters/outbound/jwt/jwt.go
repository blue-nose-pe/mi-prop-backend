// Package jwtadapter implementa ports.TokenIssuer y ports.TokenVerifier
// envolviendo a internal/shared/jwtmw. El core depende solo de los ports;
// si mañana cambiamos a otra librería de JWT, solo se toca este archivo.
package jwtadapter

import (
	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/jwtmw"
)

// Issuer envuelve un *jwtmw.Signer.
type Issuer struct {
	signer *jwtmw.Signer
}

var _ ports.TokenIssuer = (*Issuer)(nil)

func NewIssuer(s *jwtmw.Signer) *Issuer { return &Issuer{signer: s} }

func (i *Issuer) IssuePair(p ports.TokenIssueParams) (*ports.TokenPair, error) {
	pair, err := i.signer.IssuePair(jwtmw.IssueParams{
		UserID:             string(p.UserID),
		Email:              p.Email,
		Roles:              p.Roles,
		Permissions:        p.Permissions,
		SchoolID:           p.SchoolID,
		MustChangePassword: p.MustChangePassword,
	})
	if err != nil {
		return nil, err
	}
	return &ports.TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		RefreshJTI:   pair.RefreshJTI,
		AccessExp:    pair.AccessExp,
		RefreshExp:   pair.RefreshExp,
	}, nil
}

// Verifier envuelve un *jwtmw.Verifier.
type Verifier struct {
	verifier *jwtmw.Verifier
}

var _ ports.TokenVerifier = (*Verifier)(nil)

func NewVerifier(v *jwtmw.Verifier) *Verifier { return &Verifier{verifier: v} }

func (v *Verifier) Verify(token string) (*ports.TokenClaims, error) {
	c, err := v.verifier.Verify(token)
	if err != nil {
		return nil, err
	}
	exp := c.ExpiresAt
	expTime := exp.Time
	return &ports.TokenClaims{
		UserID:    domain.UserID(c.Subject),
		JTI:       c.ID,
		Type:      c.Type,
		ExpiresAt: expTime,
	}, nil
}
