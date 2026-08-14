package users

import (
	"context"
	"errors"

	"github.com/neuroguard/gateway/internal/domain"
)

var ErrNotFound = errors.New("usuario nao encontrado")

type Repository interface {
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
}
