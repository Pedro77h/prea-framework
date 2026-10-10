package user

import (
	"context"
	"prea/database"
	"prea/internal/preas/user/dtos"
	"prea/security"
	"time"
)

type Service struct {
	Repo *Repository `inject:""`
}

func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.Repo.GetAll(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (User, error) {
	return s.Repo.GetByID(ctx, id)
}

func (s *Service) GetByUsername(ctx context.Context, username string) (User, error) {
	return s.Repo.FindByUsername(ctx, username)
}

func (s *Service) Create(ctx context.Context, req dtos.CreateRequest) (User, error) {
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		return User{}, err
	}
	req.Password = hash

	return s.Repo.Create(ctx, req)
}

func (s *Service) Update(ctx context.Context, id string, req dtos.UpdateRequest) (User, error) {
	return s.Repo.Update(ctx, id, req)
}

// StartSession records the login and makes tokenID the only valid token.
func (s *Service) StartSession(ctx context.Context, id, tokenID string, at time.Time) error {
	_, err := s.Repo.Update(ctx, id, database.Args{"lastlogin": at, "token": tokenID})
	return err
}

func (s *Service) EndSession(ctx context.Context, id string) error {
	_, err := s.Repo.Update(ctx, id, database.Args{"token": nil})
	return err
}

func (s *Service) ActiveToken(ctx context.Context, id string) (string, error) {
	u, err := s.Repo.GetByID(ctx, id)
	if err != nil || u.Token == nil {
		return "", err
	}
	return *u.Token, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.Repo.Delete(ctx, id)
}
