// Package care guarda o vínculo entre paciente e cuidador (contato de
// emergência ou médico responsável), que é o que sustenta a autorização por
// relação no pacote crisis.
package care

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository satisfaz crisis.LinkChecker sem declarar uma interface
// própria: a interface mora no consumidor, não aqui.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// IsActiveLink responde se o cuidador tem vínculo ATIVO com o paciente.
// Vínculo pending (sem aceite) e revoked não contam — o paciente é quem
// autoriza quem vê e age sobre a crise dele.
//
// Lido do banco, nunca do JWT: o papel no token fica congelado até o exp, e um
// cuidador desvinculado hoje continuaria autorizado até o token vencer.
func (r *PostgresRepository) IsActiveLink(ctx context.Context, caregiverID, patientID string) (bool, error) {
	const query = `SELECT EXISTS (
		SELECT 1 FROM care_links
		 WHERE caregiver_id = $1 AND patient_id = $2 AND status = 'active')`

	var active bool
	if err := r.pool.QueryRow(ctx, query, caregiverID, patientID).Scan(&active); err != nil {
		return false, err
	}
	return active, nil
}
