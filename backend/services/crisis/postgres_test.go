package crisis

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neuroguard/gateway/database"
	"github.com/neuroguard/gateway/internal/domain"
)

// Estes testes precisam de Postgres de verdade: o que está sendo verificado é
// justamente o comportamento do banco (índice único parcial, ON CONFLICT e o
// compare-and-swap sob concorrência), que um dublê em memória não reproduz.
//
//	docker compose up -d
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/neuroguard?sslmode=disable go test ./crisis/
func setup(t *testing.T) (*PostgresRepository, *pgxpool.Pool, string, string) {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL não definida — subo o compose e defina para rodar o teste de integração")
	}

	ctx := context.Background()
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrar: %v", err)
	}

	// Usuários próprios por teste, com email único, para rodar em paralelo com
	// o seed e com os outros testes sem colidir no índice de email.
	patient, rescuer := uuid.NewString(), uuid.NewString()
	for id, role := range map[string]string{patient: "patient", rescuer: "rescuer"} {
		_, err := pool.Exec(ctx,
			`INSERT INTO users (id, name, email, password_hash, role) VALUES ($1, 'Teste', $2, 'x', $3)`,
			id, "t-"+id+"@teste.local", role)
		if err != nil {
			t.Fatalf("criar usuário: %v", err)
		}
	}

	t.Cleanup(func() {
		// Ordem importa: crises e vínculos referenciam users.
		pool.Exec(ctx, `DELETE FROM crises WHERE patient_id = $1`, patient)
		pool.Exec(ctx, `DELETE FROM care_links WHERE patient_id = $1`, patient)
		pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, []string{patient, rescuer})
		pool.Close()
	})

	return NewPostgresRepository(pool), pool, patient, rescuer
}

func countCrises(t *testing.T, pool *pgxpool.Pool, patientID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM crises WHERE patient_id = $1`, patientID).Scan(&n); err != nil {
		t.Fatalf("contar crises: %v", err)
	}
	return n
}

// O paciente em crise aperta o botão 30 vezes. Uma linha.
func TestCreateIsIdempotentForOpenCrisis(t *testing.T) {
	repo, pool, patient, _ := setup(t)
	ctx := context.Background()

	first, created, err := repo.Create(ctx, patient, nil)
	if err != nil || !created {
		t.Fatalf("primeira: created=%v err=%v", created, err)
	}

	for i := 0; i < 29; i++ {
		c, created, err := repo.Create(ctx, patient, nil)
		if err != nil {
			t.Fatalf("tentativa %d: %v", i+2, err)
		}
		if created {
			t.Fatalf("tentativa %d criou uma crise nova", i+2)
		}
		if c.ID != first.ID {
			t.Fatalf("tentativa %d devolveu %s, quero %s", i+2, c.ID, first.ID)
		}
	}

	if n := countCrises(t, pool, patient); n != 1 {
		t.Errorf("%d crises no banco, quero 1", n)
	}
}

// 30 goroutines simultâneas: o índice único parcial precisa colapsar todas.
func TestCreateUnderConcurrencyCreatesOneRow(t *testing.T) {
	repo, pool, patient, _ := setup(t)

	var wg sync.WaitGroup
	ids := make([]string, 30)
	errs := make([]error, 30)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, _, err := repo.Create(context.Background(), patient, nil)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = c.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("goroutine %d viu crise %s, a 0 viu %s", i, id, ids[0])
		}
	}
	if n := countCrises(t, pool, patient); n != 1 {
		t.Errorf("%d crises no banco, quero 1", n)
	}
}

// O replay que o índice parcial NÃO cobre: a crise já encerrou quando o
// wearable reenvia. Sem a chave de idempotência isso criaria uma crise falsa.
func TestCreateWithSameKeyAfterCloseReturnsOriginal(t *testing.T) {
	repo, pool, patient, _ := setup(t)
	ctx := context.Background()
	key := uuid.NewString()

	original, created, err := repo.Create(ctx, patient, &key)
	if err != nil || !created {
		t.Fatalf("criar: created=%v err=%v", created, err)
	}
	if err := repo.Close(ctx, original.ID, "resolved"); err != nil {
		t.Fatalf("encerrar: %v", err)
	}

	again, created, err := repo.Create(ctx, patient, &key)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created {
		t.Error("replay criou crise nova — a chave de idempotência não pegou")
	}
	if again.ID != original.ID {
		t.Errorf("replay devolveu %s, quero %s", again.ID, original.ID)
	}
	if again.Status != domain.StatusClosed {
		t.Errorf("status = %s, quero encerrada (devolve a original como está)", again.Status)
	}
	if n := countCrises(t, pool, patient); n != 1 {
		t.Errorf("%d crises no banco, quero 1", n)
	}
}

// Chave nova depois de encerrar é evento novo: aí sim cria.
func TestCreateWithNewKeyAfterCloseCreatesNewCrisis(t *testing.T) {
	repo, pool, patient, _ := setup(t)
	ctx := context.Background()

	k1, k2 := uuid.NewString(), uuid.NewString()
	first, _, err := repo.Create(ctx, patient, &k1)
	if err != nil {
		t.Fatalf("primeira: %v", err)
	}
	if err := repo.Close(ctx, first.ID, "resolved"); err != nil {
		t.Fatalf("encerrar: %v", err)
	}

	second, created, err := repo.Create(ctx, patient, &k2)
	if err != nil || !created {
		t.Fatalf("segunda: created=%v err=%v", created, err)
	}
	if second.ID == first.ID {
		t.Error("devolveu a crise antiga em vez de criar uma nova")
	}
	if n := countCrises(t, pool, patient); n != 2 {
		t.Errorf("%d crises no banco, quero 2", n)
	}
}

// Dois socorristas confirmam ao mesmo tempo: exatamente um vence.
func TestConfirmRescueUnderConcurrencyHasExactlyOneWinner(t *testing.T) {
	repo, pool, patient, rescuer := setup(t)
	ctx := context.Background()

	c, _, err := repo.Create(ctx, patient, nil)
	if err != nil {
		t.Fatalf("criar: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = repo.ConfirmRescue(context.Background(), c.ID, rescuer)
		}(i)
	}
	wg.Wait()

	vencedores, conflitos := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			vencedores++
		case errors.Is(err, ErrConflict):
			conflitos++
		default:
			t.Fatalf("goroutine %d: erro inesperado %v", i, err)
		}
	}
	if vencedores != 1 {
		t.Errorf("%d vencedores, quero exatamente 1", vencedores)
	}
	if conflitos != n-1 {
		t.Errorf("%d conflitos, quero %d", conflitos, n-1)
	}

	// E a linha tem de ficar coerente: rescuer_id e rescue_confirmed_at juntos.
	var rescuerID *string
	var confirmedAt *string
	if err := pool.QueryRow(ctx,
		`SELECT rescuer_id::text, rescue_confirmed_at::text FROM crises WHERE id = $1`, c.ID,
	).Scan(&rescuerID, &confirmedAt); err != nil {
		t.Fatalf("ler crise: %v", err)
	}
	if rescuerID == nil || confirmedAt == nil {
		t.Errorf("rescuer_id=%v rescue_confirmed_at=%v — os dois deveriam estar preenchidos", rescuerID, confirmedAt)
	}
}

func TestMutationsOnClosedCrisisConflict(t *testing.T) {
	repo, _, patient, rescuer := setup(t)
	ctx := context.Background()

	c, _, err := repo.Create(ctx, patient, nil)
	if err != nil {
		t.Fatalf("criar: %v", err)
	}
	if err := repo.Close(ctx, c.ID, "resolved"); err != nil {
		t.Fatalf("encerrar: %v", err)
	}

	casos := map[string]error{
		"activate":      repo.Activate(ctx, c.ID),
		"confirm":       repo.ConfirmRescue(ctx, c.ID, rescuer),
		"close de novo": repo.Close(ctx, c.ID, "resolved"),
	}
	for nome, err := range casos {
		if !errors.Is(err, ErrConflict) {
			t.Errorf("%s em crise encerrada: err = %v, quero ErrConflict", nome, err)
		}
	}
}

func TestActivateMovesAuraToActive(t *testing.T) {
	repo, _, patient, _ := setup(t)
	ctx := context.Background()

	c, _, err := repo.Create(ctx, patient, nil)
	if err != nil {
		t.Fatalf("criar: %v", err)
	}
	if err := repo.Activate(ctx, c.ID); err != nil {
		t.Fatalf("ativar: %v", err)
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("buscar: %v", err)
	}
	if got.Status != domain.StatusActive {
		t.Errorf("status = %s, quero ativa", got.Status)
	}
	if got.ActiveAt == nil {
		t.Error("active_at ficou nulo — a constraint crises_active_coherent exige")
	}

	// Segunda ativação não tem seta na máquina de estados.
	if err := repo.Activate(ctx, c.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("ativar de novo: err = %v, quero ErrConflict", err)
	}
}

// Id que não é UUID é id que não existe — e não um erro de sintaxe do driver
// vazando como 500.
func TestFindByIDRejectsMalformedID(t *testing.T) {
	repo, _, _, _ := setup(t)

	for _, id := range []string{"", "abc", "1", "'; DROP TABLE crises; --"} {
		if _, err := repo.FindByID(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("FindByID(%q): err = %v, quero ErrNotFound", id, err)
		}
	}
}
