package crisis

import (
	"context"
	"errors"
	"fmt"

	"github.com/neuroguard/gateway/internal/domain"
)

// ErrInvalidCloseReason é entrada inválida do cliente, não estado — vira 400.
var ErrInvalidCloseReason = errors.New("motivo de encerramento invalido")

// closeReasons espelha o CHECK crises_close_reason da migração 002.
var closeReasons = map[string]bool{
	"resolved":    true,
	"false_alarm": true,
	"timeout":     true, // reservado: hoje nada encerra por timeout
}

// Service concentra as regras de crise. Recebe o repositório e o verificador
// de vínculo por parâmetro: quem monta é o main.go, então o service é testável
// sem Postgres.
type Service struct {
	repo  Repository
	links LinkChecker
}

func NewService(repo Repository, links LinkChecker) *Service {
	return &Service{repo: repo, links: links}
}

// Trigger registra a aura. O patientID vem do token, nunca do corpo da
// requisição — se viesse do JSON, qualquer paciente dispararia aura em nome de
// outro. Idempotente: created=false quando reaproveitou crise existente.
func (s *Service) Trigger(ctx context.Context, patientID string, clientEventID *string) (*domain.Crisis, bool, error) {
	return s.repo.Create(ctx, patientID, clientEventID)
}

// Activate marca o início da crise (aura -> ativa).
func (s *Service) Activate(ctx context.Context, actorID, crisisID string) error {
	c, err := s.loadAuthorized(ctx, actorID, crisisID)
	if err != nil {
		return err
	}
	if !c.Status.CanGoTo(domain.StatusActive) {
		return fmt.Errorf("%w: crise em %s", ErrConflict, c.Status)
	}
	return s.repo.Activate(ctx, c.ID)
}

// Confirm registra que um socorrista assumiu o atendimento.
//
// Exige vínculo ativo com o paciente — o RequireRole da rota garante que o ator
// é socorrista ou médico, mas não que seja socorrista *deste* paciente. Sem
// esta checagem, qualquer socorrista cadastrado confirmaria qualquer resgate.
func (s *Service) Confirm(ctx context.Context, actorID, crisisID string) error {
	c, err := s.repo.FindByID(ctx, crisisID)
	if err != nil {
		return err
	}

	// Autorização ANTES do estado. Invertido, um ator sem vínculo receberia 409
	// numa crise encerrada e aprenderia que ela existe.
	linked, err := s.links.IsActiveLink(ctx, actorID, c.PatientID)
	if err != nil {
		return fmt.Errorf("verificar vinculo: %w", err)
	}
	if !linked {
		return ErrNotFound
	}

	if c.Status == domain.StatusClosed {
		return fmt.Errorf("%w: crise encerrada", ErrConflict)
	}
	// Primeiro socorrista vence. O CAS no repositório fecha a corrida de
	// verdade; esta checagem só dá a mensagem certa no caso comum.
	if c.Rescued() {
		return fmt.Errorf("%w: resgate ja assumido", ErrConflict)
	}

	return s.repo.ConfirmRescue(ctx, c.ID, actorID)
}

// Close encerra a crise. Pode ser o paciente ou um cuidador com vínculo ativo.
func (s *Service) Close(ctx context.Context, actorID, crisisID, reason string) error {
	// Valida a entrada antes de tocar no banco.
	if !closeReasons[reason] {
		return fmt.Errorf("%w: %q", ErrInvalidCloseReason, reason)
	}

	c, err := s.loadAuthorized(ctx, actorID, crisisID)
	if err != nil {
		return err
	}
	if !c.Status.CanGoTo(domain.StatusClosed) {
		return fmt.Errorf("%w: crise em %s", ErrConflict, c.Status)
	}
	return s.repo.Close(ctx, c.ID, reason)
}

// loadAuthorized carrega a crise e exige que o ator seja o paciente ou um
// cuidador com vínculo ativo. Devolve ErrNotFound nos dois casos de negativa
// (crise inexistente e ator sem relação), para não vazar existência.
func (s *Service) loadAuthorized(ctx context.Context, actorID, crisisID string) (*domain.Crisis, error) {
	c, err := s.repo.FindByID(ctx, crisisID)
	if err != nil {
		return nil, err
	}

	// O paciente não tem vínculo consigo mesmo (care_links_not_self), então
	// este caso é resolvido antes de consultar o banco.
	if actorID == c.PatientID {
		return c, nil
	}

	linked, err := s.links.IsActiveLink(ctx, actorID, c.PatientID)
	if err != nil {
		return nil, fmt.Errorf("verificar vinculo: %w", err)
	}
	if !linked {
		return nil, ErrNotFound
	}
	return c, nil
}
