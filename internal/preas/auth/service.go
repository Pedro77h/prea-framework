package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"prea/common"
	"prea/database"
	"prea/internal/preas/auth/dtos"
	"prea/internal/preas/user"
	"prea/security"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = time.Hour

type Service struct {
	database.Transactional
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
	tokenID := rand.Text()
	// LastLogin carries the previous login, before this one.
	payload := dtos.Payload{ID: u.ID, Username: u.Username, Profile: u.Profile, LastLogin: u.LastLogin}
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		Subject:   u.Username,
		ID:        tokenID,
	}

	// Starting the session revokes the previous one: if the token can't be
	// created, the rollback keeps the user logged in where they were.
	var token string
	err = s.Transaction(ctx, func(ctx context.Context) error {
		if err := s.Users.StartSession(ctx, u.ID, tokenID, now); err != nil {
			return err
		}
		token, err = security.Jwt[dtos.Payload]{}.CreateToken(payload, claims)
		return err
	})
	if err != nil {
		return dtos.Session{}, err
	}
	return dtos.Session{Token: token, Payload: payload}, nil
}

func (s *Service) Validate(ctx context.Context, token string) (security.DataClaims[dtos.Payload], error) {
	claims, err := security.Jwt[dtos.Payload]{}.DecodeToken(token)
	if err != nil {
		return claims, fmt.Errorf("%w: invalid token", common.ErrUnauthorized)
	}
	if active, err := s.Users.ActiveToken(ctx, claims.Payload.ID); err != nil || active != claims.ID {
		return claims, fmt.Errorf("%w: session ended", common.ErrUnauthorized)
	}
	return claims, nil
}

func (s *Service) Logout(ctx context.Context, userID string) error {
	return s.Users.EndSession(ctx, userID)
}
