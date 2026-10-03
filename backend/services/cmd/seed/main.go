// Comando seed cria um usuário com senha hasheada.
// Existe porque não há endpoint de cadastro: quem pode criar um doctor ou um
// rescuer é uma decisão de produto ainda aberta, e enquanto isso o acesso
// inicial não pode depender de SQL manual com hash colado à mão.
//
// Uso:
//
//	go run ./cmd/seed -email ana@exemplo.com -password senha123 -name "Ana" -role patient
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/neuroguard/gateway/database"
	"github.com/neuroguard/gateway/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	email := flag.String("email", "", "email do usuário (obrigatório)")
	password := flag.String("password", "", "senha em texto plano (obrigatório)")
	name := flag.String("name", "", "nome do usuário (obrigatório)")
	role := flag.String("role", string(domain.RolePatient), "patient | rescuer | doctor | admin")
	flag.Parse()

	if err := run(*email, *password, *name, *role); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func run(email, password, name, role string) error {
	// A constraint users_email_normalized no banco exige email já normalizado;
	// normalizar aqui evita um erro de constraint em vez de um insert válido.
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)

	if email == "" || password == "" || name == "" {
		return errors.New("-email, -password e -name são obrigatórios")
	}
	if !validRole(role) {
		return fmt.Errorf("role inválido: %q (use patient, rescuer, doctor ou admin)", role)
	}
	// bcrypt trunca silenciosamente em 72 bytes: senha mais longa teria o final
	// ignorado, e o usuário entraria com uma senha que não é a que ele digitou.
	if len(password) > 72 {
		return errors.New("senha excede 72 bytes (limite do bcrypt)")
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

	// O seed roda antes do gateway na primeira execução, então a tabela users
	// pode não existir ainda.
	if err := database.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("aplicar migrações: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("gerar hash da senha: %w", err)
	}

	id := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, $2, $3, $4, $5)`,
		id, name, email, string(hash), role,
	)
	// 23505 = unique_violation. Mensagem clara em vez do erro cru do driver.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("já existe usuário com o email %s", email)
	}
	if err != nil {
		return fmt.Errorf("inserir usuário: %w", err)
	}

	log.Printf("usuário criado: %s (%s, role=%s)", email, id, role)
	return nil
}

func validRole(role string) bool {
	switch domain.Role(role) {
	case domain.RolePatient, domain.RoleRescuer, domain.RoleDoctor, domain.RoleAdmin:
		return true
	}
	return false
}
