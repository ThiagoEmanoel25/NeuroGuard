package crisis

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neuroguard/gateway/internal/domain"
)

// colunas na ordem que scanCrisis espera. ::text nos uuid porque o domínio
// trabalha com string, igual a users.PostgresRepository.
const colunas = `id::text, patient_id::text, status, client_event_id::text,
	rescuer_id::text, aura_at, active_at, rescue_confirmed_at, closed_at, close_reason`

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func scanCrisis(row pgx.Row) (*domain.Crisis, error) {
	var c domain.Crisis
	err := row.Scan(
		&c.ID, &c.PatientID, &c.Status, &c.ClientEventID,
		&c.RescuerID, &c.AuraAt, &c.ActiveAt, &c.RescueConfirmedAt,
		&c.ClosedAt, &c.CloseReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Create é o INSERT idempotente.
//
// ON CONFLICT DO NOTHING sem alvo cobre de uma vez os DOIS índices únicos:
// crises_one_open_per_patient (o paciente aperta o botão 30 vezes) e
// crises_idempotency (o wearable reenvia um evento antigo). Se inseriu,
// RETURNING devolve a linha; se conflitou, volta vazio e buscamos a existente.
//
// Em READ COMMITTED, duas requisições concorrentes não criam duas crises: a
// perdedora espera a vencedora comitar, cai no DO NOTHING, e o SELECT seguinte
// já enxerga a linha. Quem resolve a corrida é o Postgres.
func (r *PostgresRepository) Create(ctx context.Context, patientID string, clientEventID *string) (*domain.Crisis, bool, error) {
	if _, err := uuid.Parse(patientID); err != nil {
		return nil, false, fmt.Errorf("patient_id invalido: %w", err)
	}

	c, err := scanCrisis(r.pool.QueryRow(ctx,
		`INSERT INTO crises (patient_id, client_event_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING RETURNING `+colunas,
		patientID, clientEventID))
	if err == nil {
		return c, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	// Conflitou. Pela chave primeiro: é o caso em que o cliente reenviou o
	// mesmo evento, e a crise correspondente pode até já estar encerrada.
	if clientEventID != nil {
		c, err := scanCrisis(r.pool.QueryRow(ctx,
			`SELECT `+colunas+` FROM crises WHERE patient_id = $1 AND client_event_id = $2`,
			patientID, clientEventID))
		if err == nil {
			return c, false, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
		// Chave nova, mas o paciente já tem crise aberta (com outra chave):
		// o conflito foi no outro índice. Cai no SELECT abaixo.
	}

	c, err = scanCrisis(r.pool.QueryRow(ctx,
		`SELECT `+colunas+` FROM crises WHERE patient_id = $1 AND status <> 'encerrada'`,
		patientID))
	if errors.Is(err, ErrNotFound) {
		// A crise aberta foi encerrada entre o INSERT e este SELECT. Estado
		// mudou debaixo de nós; o cliente pode repetir a chamada.
		return nil, false, fmt.Errorf("%w: crise mudou durante a criacao", ErrConflict)
	}
	if err != nil {
		return nil, false, err
	}
	return c, false, nil
}

// FindByID devolve ErrNotFound também quando o id não é um UUID válido.
// A tradução fica aqui, e não em cada handler, porque todo caminho de leitura
// passa por este método — id inválido é id que não existe.
func (r *PostgresRepository) FindByID(ctx context.Context, id string) (*domain.Crisis, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrNotFound
	}
	return scanCrisis(r.pool.QueryRow(ctx,
		`SELECT `+colunas+` FROM crises WHERE id = $1`, id))
}

// exec roda um UPDATE de compare-and-swap: a condição de estado está no WHERE,
// então o Postgres serializa as concorrentes e só uma casa a linha.
// Zero linhas afetadas = a crise não está no estado exigido (ou não existe).
func (r *PostgresRepository) exec(ctx context.Context, id, sql string, args ...any) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := r.pool.Exec(ctx, sql, append([]any{id}, args...)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (r *PostgresRepository) Activate(ctx context.Context, id string) error {
	return r.exec(ctx, id,
		`UPDATE crises SET status = 'ativa', active_at = now()
		  WHERE id = $1 AND status = 'aura'`)
}

// ConfirmRescue fecha a corrida entre socorristas: o WHERE exige
// rescuer_id IS NULL, então o primeiro UPDATE vence e o segundo afeta zero
// linhas. Primeiro vence, sem lock em Go.
func (r *PostgresRepository) ConfirmRescue(ctx context.Context, id, rescuerID string) error {
	if _, err := uuid.Parse(rescuerID); err != nil {
		return fmt.Errorf("rescuer_id invalido: %w", err)
	}
	return r.exec(ctx, id,
		`UPDATE crises SET rescuer_id = $2, rescue_confirmed_at = now()
		  WHERE id = $1 AND status <> 'encerrada' AND rescuer_id IS NULL`,
		rescuerID)
}

func (r *PostgresRepository) Close(ctx context.Context, id, reason string) error {
	return r.exec(ctx, id,
		`UPDATE crises SET status = 'encerrada', closed_at = now(), close_reason = $2
		  WHERE id = $1 AND status <> 'encerrada'`,
		reason)
}
