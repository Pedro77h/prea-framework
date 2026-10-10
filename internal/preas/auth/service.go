package auth

import (
	"context"
	"errors"
	"fmt"
	"prea/common"
	"prea/internal/preas/auth/dtos"
	"prea/internal/preas/user"
	"prea/security"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = time.Hour

type Service struct {
	Users *user.Service `inject:""`
}

func (s *Service) Login(ctx context.Context, username, password string) (dtos.Session, error) {
	u, err := s.Users.GetByUsername(ctx, username)
	if errors.Is(err, common.ErrNotFound) {
		return dtos.Session{}, fmt.Errorf("%w: invalid credentials", common.ErrUnauthorized)
	}
	if err != nil {
		return dtos.Session{}, err
	}

	if ok, err := security.ComparePassword(password, u.Password); err != nil || !ok {
		return dtos.Session{}, fmt.Errorf("%w: invalid credentials", common.ErrUnauthorized)
	}

	now := time.Now()
	if err := s.Users.TouchLastLogin(ctx, u.ID, now); err != nil {
		return dtos.Session{}, err
	}

	// LastLogin carries the previous login, before this one.
	payload := dtos.Payload{Username: u.Username, Profile: u.Profile, LastLogin: u.LastLogin}
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		Subject:   u.Username,
	}

	token, err := security.Jwt[dtos.Payload]{}.CreateToken(payload, claims)
	if err != nil {
		return dtos.Session{}, err
	}
	return dtos.Session{Token: token, Payload: payload}, nil
}

func (s *Service) Validate(token string) (security.DataClaims[dtos.Payload], error) {
	claims, err := security.Jwt[dtos.Payload]{}.DecodeToken(token)
	if err != nil {
		return claims, fmt.Errorf("%w: invalid token", common.ErrUnauthorized)
	}
	return claims, nil
}
