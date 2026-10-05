// Comando seedlink cria um vínculo ativo entre paciente e cuidador.
//
// Existe porque não há endpoint de vínculo: quem convida e quem aceita é
// decisão de produto ainda aberta (na Fase 2 o paciente convida e o cuidador
// aceita). Sem vínculo, nenhum socorrista consegue confirmar resgate, então o
// ReBAC fica impossível de exercitar à mão.
//
// Uso:
//
//	go run ./cmd/seedlink -patient ana@exemplo.com -caregiver bruno@exemplo.com -kind emergency_contact
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neuroguard/gateway/database"
	"github.com/neuroguard/gateway/internal/domain"
)

func main() {
	patient := flag.String("patient", "", "email do paciente (obrigatório)")
	caregiver := flag.String("caregiver", "", "email do cuidador (obrigatório)")
	kind := flag.String("kind", "emergency_contact", "emergency_contact | responsible_doctor")
	priority := flag.Int("priority", 1, "ordem de acionamento; 1 é chamado primeiro")
	flag.Parse()

	if err := run(*patient, *caregiver, *kind, *priority); err != nil {
		log.Fatalf("seedlink: %v", err)
	}
}

func run(patientEmail, caregiverEmail, kind string, priority int) error {
	patientEmail = strings.ToLower(strings.TrimSpace(patientEmail))
	caregiverEmail = strings.ToLower(strings.TrimSpace(caregiverEmail))

	if patientEmail == "" || caregiverEmail == "" {
		return errors.New("-patient e -caregiver são obrigatórios")
	}
	if kind != "emergency_contact" && kind != "responsible_doctor" {
		return fmt.Errorf("kind inválido: %q (use emergency_contact ou responsible_doctor)", kind)
	}
	if priority < 1 {
		return errors.New("-priority deve ser 1 ou maior")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/neuroguard?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("conectar ao PostgreSQL: %w", err)
	}
	defer pool.Close()

	patientID, patientRole, err := lookup(ctx, pool, patientEmail)
	if err != nil {
		return err
	}
	caregiverID, caregiverRole, err := lookup(ctx, pool, caregiverEmail)
	if err != nil {
		return err
	}

	// Regras entre tabelas que o CHECK do Postgres não alcança — um CHECK só
	// enxerga a própria linha, então estas moram aqui.
	if patientRole != domain.RolePatient {
		return fmt.Errorf("%s tem role %s; o vínculo exige um paciente", patientEmail, patientRole)
	}
	if kind == "responsible_doctor" && caregiverRole != domain.RoleDoctor {
		return fmt.Errorf("%s tem role %s; responsible_doctor exige um doctor", caregiverEmail, caregiverRole)
	}
	if kind == "emergency_contact" && caregiverRole == domain.RolePatient {
		return fmt.Errorf("%s é paciente; contato de emergência deve ser rescuer ou doctor", caregiverEmail)
	}

	// Já nasce active: o seed representa o aceite que na Fase 2 vem do próprio
	// cuidador. accepted_at junto por causa de care_links_accepted_coherent.
	_, err = pool.Exec(ctx,
		`INSERT INTO care_links (patient_id, caregiver_id, kind, priority, status, accepted_at)
		 VALUES ($1, $2, $3, $4, 'active', now())`,
		patientID, caregiverID, kind, priority)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("já existe vínculo entre %s e %s", patientEmail, caregiverEmail)
	}
	if err != nil {
		return fmt.Errorf("inserir vínculo: %w", err)
	}

	log.Printf("vínculo ativo criado: %s <- %s (%s, priority=%d)", patientEmail, caregiverEmail, kind, priority)
	return nil
}

func lookup(ctx context.Context, pool *pgxpool.Pool, email string) (id string, role domain.Role, err error) {
	err = pool.QueryRow(ctx,
		`SELECT id::text, role FROM users WHERE lower(email) = lower($1)`, email,
	).Scan(&id, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("usuário não encontrado: %s (crie com ./cmd/seed)", email)
	}
	if err != nil {
		return "", "", fmt.Errorf("buscar %s: %w", email, err)
	}
	return id, role, nil
}
