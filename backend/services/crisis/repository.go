package crisis

import (
	"context"
	"errors"

	"github.com/neuroguard/gateway/internal/domain"
)

var (
	// ErrNotFound cobre dois casos de propósito: a crise não existe, ou existe
	// e o ator não tem vínculo com o paciente. O handler traduz os dois em 404,
	// porque 403 confirmaria a existência de um recurso que o ator não deveria
	// nem saber que existe.
	ErrNotFound = errors.New("crise nao encontrada")

	// ErrConflict é estado incompatível com a ação: transição que a máquina de
	// estados não permite, ou resgate que outro socorrista já assumiu.
	// Vira 409 no HTTP.
	ErrConflict = errors.New("estado da crise incompativel com a operacao")
)

// Repository é o acesso a dados de crise. As mutações são compare-and-swap:
// a condição de estado vai no WHERE do UPDATE, então duas requisições
// concorrentes não precisam de lock em Go — o Postgres serializa, e a
// perdedora recebe ErrConflict.
type Repository interface {
	// Create grava a aura. É idempotente: devolve created=false e a crise
	// existente quando o paciente já tem crise aberta, ou quando clientEventID
	// repete uma chave já usada (replay do wearable).
	Create(ctx context.Context, patientID string, clientEventID *string) (c *domain.Crisis, created bool, err error)

	FindByID(ctx context.Context, id string) (*domain.Crisis, error)

	// Activate move aura -> ativa.
	Activate(ctx context.Context, id string) error

	// ConfirmRescue preenche rescuer_id e rescue_confirmed_at. Não é transição
	// de fase: resgate é eixo ortogonal. Primeiro socorrista vence; os demais
	// recebem ErrConflict.
	ConfirmRescue(ctx context.Context, id, rescuerID string) error

	// Close move a fase atual -> encerrada.
	Close(ctx context.Context, id, reason string) error
}

// LinkChecker é a única coisa que o service precisa saber sobre vínculos.
// Interface de um método, definida aqui (no consumidor) e não no pacote care:
// o service não deve alcançar o repositório de vínculos inteiro.
type LinkChecker interface {
	IsActiveLink(ctx context.Context, caregiverID, patientID string) (bool, error)
}
