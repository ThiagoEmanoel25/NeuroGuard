package crisis

import (
	"context"
	"errors"
	"testing"

	"github.com/neuroguard/gateway/internal/domain"
)

// ─── Dublês ─────────────────────────────────────────────────────────────────
// Repositório em memória: o service é regra de negócio, e testá-lo contra
// Postgres só tornaria o teste lento sem provar nada a mais. O CAS de verdade
// é coberto pelo teste de integração em postgres_test.go.

type fakeRepo struct {
	crises map[string]*domain.Crisis
	// forçam o caminho de erro do CAS (crise mudou entre o SELECT e o UPDATE)
	activateErr, confirmErr, closeErr error
}

func newFakeRepo(cs ...*domain.Crisis) *fakeRepo {
	r := &fakeRepo{crises: map[string]*domain.Crisis{}}
	for _, c := range cs {
		r.crises[c.ID] = c
	}
	return r
}

func (r *fakeRepo) Create(_ context.Context, patientID string, key *string) (*domain.Crisis, bool, error) {
	for _, c := range r.crises {
		if c.PatientID == patientID && c.Status != domain.StatusClosed {
			return c, false, nil
		}
	}
	c := &domain.Crisis{ID: "nova", PatientID: patientID, Status: domain.StatusAura, ClientEventID: key}
	r.crises[c.ID] = c
	return c, true, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id string) (*domain.Crisis, error) {
	c, ok := r.crises[id]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) Activate(_ context.Context, id string) error {
	if r.activateErr != nil {
		return r.activateErr
	}
	r.crises[id].Status = domain.StatusActive
	return nil
}

func (r *fakeRepo) ConfirmRescue(_ context.Context, id, rescuerID string) error {
	if r.confirmErr != nil {
		return r.confirmErr
	}
	r.crises[id].RescuerID = &rescuerID
	return nil
}

func (r *fakeRepo) Close(_ context.Context, id, reason string) error {
	if r.closeErr != nil {
		return r.closeErr
	}
	r.crises[id].Status = domain.StatusClosed
	r.crises[id].CloseReason = &reason
	return nil
}

// fakeLinks responde o vínculo. `calls` prova que o service realmente
// consultou — sem isso o teste passaria mesmo se o ReBAC fosse esquecido.
type fakeLinks struct {
	active map[string]bool // "caregiver|patient" -> ativo
	err    error
	calls  int
}

func (l *fakeLinks) IsActiveLink(_ context.Context, caregiverID, patientID string) (bool, error) {
	l.calls++
	if l.err != nil {
		return false, l.err
	}
	return l.active[caregiverID+"|"+patientID], nil
}

const (
	paciente   = "p1"
	socorrista = "r1"
	estranho   = "x9"
)

func openCrisis() *domain.Crisis {
	return &domain.Crisis{ID: "c1", PatientID: paciente, Status: domain.StatusAura}
}

// ─── Trigger ────────────────────────────────────────────────────────────────

func TestTriggerCreatesOnceAndReusesOpenCrisis(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakeLinks{})

	c, created, err := svc.Trigger(context.Background(), paciente, nil)
	if err != nil || !created {
		t.Fatalf("primeira aura: created=%v err=%v", created, err)
	}
	if c.Status != domain.StatusAura {
		t.Errorf("status = %s, quero aura", c.Status)
	}

	// O paciente em crise aperta o botão de novo: mesma crise, created=false.
	again, created, err := svc.Trigger(context.Background(), paciente, nil)
	if err != nil {
		t.Fatalf("segunda aura: %v", err)
	}
	if created {
		t.Error("segunda aura criou outra crise — deveria reaproveitar a aberta")
	}
	if again.ID != c.ID {
		t.Errorf("devolveu crise %s, quero a mesma (%s)", again.ID, c.ID)
	}
}

// ─── Confirm: o ReBAC ───────────────────────────────────────────────────────

func TestConfirmRequiresActiveLink(t *testing.T) {
	repo := newFakeRepo(openCrisis())
	links := &fakeLinks{active: map[string]bool{socorrista + "|" + paciente: true}}
	svc := NewService(repo, links)

	// Socorrista sem vínculo: 404, não 403 — não vaza que a crise existe.
	if err := svc.Confirm(context.Background(), estranho, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("sem vínculo: err = %v, quero ErrNotFound", err)
	}
	if repo.crises["c1"].Rescued() {
		t.Error("socorrista sem vínculo conseguiu assumir o resgate")
	}

	// Com vínculo ativo: passa.
	if err := svc.Confirm(context.Background(), socorrista, "c1"); err != nil {
		t.Fatalf("com vínculo: %v", err)
	}
	if got := repo.crises["c1"].RescuerID; got == nil || *got != socorrista {
		t.Errorf("rescuer_id = %v, quero %s", got, socorrista)
	}
}

func TestConfirmChecksAuthorizationBeforeState(t *testing.T) {
	// Crise já encerrada: se o service checasse o estado primeiro, devolveria
	// 409 e com isso contaria ao estranho que a crise existe. A autorização
	// vem antes, então ele recebe 404.
	closed := openCrisis()
	closed.Status = domain.StatusClosed
	svc := NewService(newFakeRepo(closed), &fakeLinks{})

	if err := svc.Confirm(context.Background(), estranho, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, quero ErrNotFound (não ErrConflict)", err)
	}
}

func TestConfirmOnClosedCrisisConflicts(t *testing.T) {
	closed := openCrisis()
	closed.Status = domain.StatusClosed
	links := &fakeLinks{active: map[string]bool{socorrista + "|" + paciente: true}}
	svc := NewService(newFakeRepo(closed), links)

	if err := svc.Confirm(context.Background(), socorrista, "c1"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, quero ErrConflict", err)
	}
}

func TestConfirmRejectsSecondRescuer(t *testing.T) {
	c := openCrisis()
	primeiro := "r0"
	c.RescuerID = &primeiro
	links := &fakeLinks{active: map[string]bool{socorrista + "|" + paciente: true}}
	svc := NewService(newFakeRepo(c), links)

	// Primeiro vence: o segundo recebe 409.
	if err := svc.Confirm(context.Background(), socorrista, "c1"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, quero ErrConflict", err)
	}
	if *c.RescuerID != primeiro {
		t.Error("o segundo socorrista sobrescreveu o primeiro")
	}
}

func TestConfirmOnUnknownCrisisDoesNotQueryLinks(t *testing.T) {
	links := &fakeLinks{}
	svc := NewService(newFakeRepo(), links)

	if err := svc.Confirm(context.Background(), socorrista, "inexistente"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, quero ErrNotFound", err)
	}
	if links.calls != 0 {
		t.Errorf("consultou vínculo %d vez(es) para crise inexistente", links.calls)
	}
}

// ─── Activate e Close: paciente ou cuidador vinculado ───────────────────────

func TestActivateAllowsPatientAndLinkedCaregiver(t *testing.T) {
	links := &fakeLinks{active: map[string]bool{socorrista + "|" + paciente: true}}

	// O próprio paciente não precisa de vínculo consigo mesmo.
	repo := newFakeRepo(openCrisis())
	svc := NewService(repo, links)
	if err := svc.Activate(context.Background(), paciente, "c1"); err != nil {
		t.Fatalf("paciente: %v", err)
	}
	if repo.crises["c1"].Status != domain.StatusActive {
		t.Errorf("status = %s, quero ativa", repo.crises["c1"].Status)
	}

	// Cuidador vinculado também pode.
	repo2 := newFakeRepo(openCrisis())
	svc2 := NewService(repo2, links)
	if err := svc2.Activate(context.Background(), socorrista, "c1"); err != nil {
		t.Fatalf("cuidador vinculado: %v", err)
	}

	// Estranho, não.
	repo3 := newFakeRepo(openCrisis())
	svc3 := NewService(repo3, links)
	if err := svc3.Activate(context.Background(), estranho, "c1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("estranho: err = %v, quero ErrNotFound", err)
	}
}

func TestActivateRejectsInvalidTransition(t *testing.T) {
	// ativa -> ativa não existe na máquina de estados.
	c := openCrisis()
	c.Status = domain.StatusActive
	svc := NewService(newFakeRepo(c), &fakeLinks{})

	if err := svc.Activate(context.Background(), paciente, "c1"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, quero ErrConflict", err)
	}
}

func TestCloseSetsReasonAndRejectsUnknownReason(t *testing.T) {
	repo := newFakeRepo(openCrisis())
	svc := NewService(repo, &fakeLinks{})

	if err := svc.Close(context.Background(), paciente, "c1", "banana"); err == nil {
		t.Error("aceitou close_reason inválido")
	}
	if repo.crises["c1"].Status == domain.StatusClosed {
		t.Error("encerrou a crise apesar do motivo inválido")
	}

	if err := svc.Close(context.Background(), paciente, "c1", "false_alarm"); err != nil {
		t.Fatalf("false_alarm: %v", err)
	}
	if got := repo.crises["c1"].CloseReason; got == nil || *got != "false_alarm" {
		t.Errorf("close_reason = %v", got)
	}
}

func TestCloseOnAlreadyClosedConflicts(t *testing.T) {
	c := openCrisis()
	c.Status = domain.StatusClosed
	svc := NewService(newFakeRepo(c), &fakeLinks{})

	if err := svc.Close(context.Background(), paciente, "c1", "resolved"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, quero ErrConflict", err)
	}
}

// ─── Erros propagados ───────────────────────────────────────────────────────

func TestLinkCheckErrorIsNotTreatedAsDenial(t *testing.T) {
	// Banco fora do ar não pode virar 404: isso esconderia a falha e, pior,
	// faria um cuidador legítimo achar que a crise não existe.
	boom := errors.New("conexao perdida")
	svc := NewService(newFakeRepo(openCrisis()), &fakeLinks{err: boom})

	err := svc.Confirm(context.Background(), socorrista, "c1")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, quero o erro do banco", err)
	}
}

func TestCASConflictFromRepoIsPropagated(t *testing.T) {
	// A crise mudou entre o FindByID e o UPDATE: o CAS não casa nenhuma linha
	// e o repositório devolve ErrConflict. O service não pode engolir isso.
	repo := newFakeRepo(openCrisis())
	repo.confirmErr = ErrConflict
	links := &fakeLinks{active: map[string]bool{socorrista + "|" + paciente: true}}
	svc := NewService(repo, links)

	if err := svc.Confirm(context.Background(), socorrista, "c1"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v, quero ErrConflict", err)
	}
}
