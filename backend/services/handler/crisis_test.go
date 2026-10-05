package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/crisis"
	"github.com/neuroguard/gateway/internal/domain"
)

// Dublês do pacote crisis: aqui o que se testa é a tradução HTTP (status code,
// corpo, leitura do token e do header), não a regra de negócio nem o SQL.

type fakeCrisisRepo struct {
	crises map[string]*domain.Crisis
	open   *domain.Crisis // devolvido por Create quando já existe crise aberta
}

func (r *fakeCrisisRepo) Create(_ context.Context, patientID string, key *string) (*domain.Crisis, bool, error) {
	if r.open != nil {
		return r.open, false, nil
	}
	c := &domain.Crisis{ID: "c-nova", PatientID: patientID, Status: domain.StatusAura, ClientEventID: key}
	r.crises[c.ID] = c
	return c, true, nil
}

func (r *fakeCrisisRepo) FindByID(_ context.Context, id string) (*domain.Crisis, error) {
	c, ok := r.crises[id]
	if !ok {
		return nil, crisis.ErrNotFound
	}
	return c, nil
}

func (r *fakeCrisisRepo) Activate(_ context.Context, id string) error {
	r.crises[id].Status = domain.StatusActive
	return nil
}

func (r *fakeCrisisRepo) ConfirmRescue(_ context.Context, id, rescuerID string) error {
	r.crises[id].RescuerID = &rescuerID
	return nil
}

func (r *fakeCrisisRepo) Close(_ context.Context, id, reason string) error {
	r.crises[id].Status = domain.StatusClosed
	r.crises[id].CloseReason = &reason
	return nil
}

type fakeLinkChecker struct{ active map[string]bool }

func (l *fakeLinkChecker) IsActiveLink(_ context.Context, caregiverID, patientID string) (bool, error) {
	return l.active[caregiverID+"|"+patientID], nil
}

const (
	testPatientID = "u-001"
	testRescuerID = "u-002"
)

// newCrisisRepo devolve um repositório com uma crise em aura do paciente de
// teste, e o socorrista de teste vinculado a ele.
func newCrisisRepo() (*fakeCrisisRepo, *fakeLinkChecker) {
	repo := &fakeCrisisRepo{crises: map[string]*domain.Crisis{
		"c-1": {ID: "c-1", PatientID: testPatientID, Status: domain.StatusAura},
	}}
	links := &fakeLinkChecker{active: map[string]bool{testRescuerID + "|" + testPatientID: true}}
	return repo, links
}

func tokenFor(userID string, role domain.Role) string {
	t, _ := auth.NewService("test-secret", time.Hour).GenerateToken(userID, role)
	return t
}

func do(t *testing.T, app *fiber.App, method, path, token, body string) (int, map[string]any) {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &payload)
	return resp.StatusCode, payload
}

func TestTriggerAuraIsIdempotentInResponse(t *testing.T) {
	repo, links := newCrisisRepo()
	app := newTestAppWith(crisis.NewService(repo, links))
	token := tokenFor(testPatientID, domain.RolePatient)

	status, body := do(t, app, http.MethodPost, "/crises", token, "")
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, quero 202", status)
	}
	if body["already_open"] != false {
		t.Errorf("already_open = %v, quero false na primeira", body["already_open"])
	}
	if body["crisis_id"] == nil || body["status"] != "aura" {
		t.Errorf("corpo inesperado: %v", body)
	}

	// Segunda chamada com crise já aberta: ainda 202, mas already_open=true.
	repo.open = repo.crises["c-1"]
	status, body = do(t, app, http.MethodPost, "/crises", token, "")
	if status != http.StatusAccepted {
		t.Errorf("status = %d, quero 202 também na repetição", status)
	}
	if body["already_open"] != true {
		t.Errorf("already_open = %v, quero true", body["already_open"])
	}
}

func TestTriggerAuraRejectsMalformedIdempotencyKey(t *testing.T) {
	repo, links := newCrisisRepo()
	app := newTestAppWith(crisis.NewService(repo, links))

	req := httptest.NewRequest(http.MethodPost, "/crises", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor(testPatientID, domain.RolePatient))
	req.Header.Set("Idempotency-Key", "nao-é-uuid")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, quero 400", resp.StatusCode)
	}
}

// Só paciente dispara a própria aura.
func TestTriggerAuraForbiddenForRescuer(t *testing.T) {
	repo, links := newCrisisRepo()
	app := newTestAppWith(crisis.NewService(repo, links))

	status, _ := do(t, app, http.MethodPost, "/crises", tokenFor(testRescuerID, domain.RoleRescuer), "")
	if status != http.StatusForbidden {
		t.Errorf("status = %d, quero 403", status)
	}
}

func TestConfirmRescueStatusCodes(t *testing.T) {
	casos := []struct {
		nome   string
		path   string
		userID string
		role   domain.Role
		want   int
	}{
		{"socorrista vinculado", "/crises/c-1/confirm", testRescuerID, domain.RoleRescuer, http.StatusAccepted},
		{"socorrista sem vínculo", "/crises/c-1/confirm", "u-estranho", domain.RoleRescuer, http.StatusNotFound},
		{"paciente não confirma", "/crises/c-1/confirm", testPatientID, domain.RolePatient, http.StatusForbidden},
		{"crise inexistente", "/crises/c-999/confirm", testRescuerID, domain.RoleRescuer, http.StatusNotFound},
	}

	for _, tc := range casos {
		t.Run(tc.nome, func(t *testing.T) {
			repo, links := newCrisisRepo()
			app := newTestAppWith(crisis.NewService(repo, links))

			status, _ := do(t, app, http.MethodPost, tc.path, tokenFor(tc.userID, tc.role), "")
			if status != tc.want {
				t.Errorf("status = %d, quero %d", status, tc.want)
			}
		})
	}
}

// Crise encerrada: 409, porque o ator tem vínculo e portanto pode saber que ela
// existe. Sem vínculo o mesmo caso daria 404.
func TestConfirmOnClosedCrisisIsConflict(t *testing.T) {
	repo, links := newCrisisRepo()
	repo.crises["c-1"].Status = domain.StatusClosed
	app := newTestAppWith(crisis.NewService(repo, links))

	status, _ := do(t, app, http.MethodPost, "/crises/c-1/confirm", tokenFor(testRescuerID, domain.RoleRescuer), "")
	if status != http.StatusConflict {
		t.Errorf("status = %d, quero 409", status)
	}
}

func TestCloseCrisis(t *testing.T) {
	t.Run("sem corpo usa resolved", func(t *testing.T) {
		repo, links := newCrisisRepo()
		app := newTestAppWith(crisis.NewService(repo, links))

		status, body := do(t, app, http.MethodPost, "/crises/c-1/close", tokenFor(testPatientID, domain.RolePatient), "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, quero 200", status)
		}
		if body["reason"] != "resolved" {
			t.Errorf("reason = %v, quero resolved", body["reason"])
		}
	})

	t.Run("false_alarm", func(t *testing.T) {
		repo, links := newCrisisRepo()
		app := newTestAppWith(crisis.NewService(repo, links))

		status, body := do(t, app, http.MethodPost, "/crises/c-1/close",
			tokenFor(testPatientID, domain.RolePatient), `{"reason":"false_alarm"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, quero 200", status)
		}
		if body["reason"] != "false_alarm" {
			t.Errorf("reason = %v", body["reason"])
		}
	})

	t.Run("motivo inválido é 400", func(t *testing.T) {
		repo, links := newCrisisRepo()
		app := newTestAppWith(crisis.NewService(repo, links))

		status, _ := do(t, app, http.MethodPost, "/crises/c-1/close",
			tokenFor(testPatientID, domain.RolePatient), `{"reason":"banana"}`)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, quero 400", status)
		}
		if repo.crises["c-1"].Status == domain.StatusClosed {
			t.Error("encerrou apesar do motivo inválido")
		}
	})

	t.Run("estranho é 404", func(t *testing.T) {
		repo, links := newCrisisRepo()
		app := newTestAppWith(crisis.NewService(repo, links))

		status, _ := do(t, app, http.MethodPost, "/crises/c-1/close", tokenFor("u-estranho", domain.RoleDoctor), "")
		if status != http.StatusNotFound {
			t.Errorf("status = %d, quero 404", status)
		}
	})
}

func TestActivateCrisis(t *testing.T) {
	repo, links := newCrisisRepo()
	app := newTestAppWith(crisis.NewService(repo, links))

	status, _ := do(t, app, http.MethodPost, "/crises/c-1/activate", tokenFor(testPatientID, domain.RolePatient), "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, quero 200", status)
	}
	if repo.crises["c-1"].Status != domain.StatusActive {
		t.Errorf("status da crise = %s, quero ativa", repo.crises["c-1"].Status)
	}

	// Segunda ativação não tem seta na máquina de estados: 409.
	status, _ = do(t, app, http.MethodPost, "/crises/c-1/activate", tokenFor(testPatientID, domain.RolePatient), "")
	if status != http.StatusConflict {
		t.Errorf("segunda ativação: status = %d, quero 409", status)
	}
}

// Sem token nenhuma rota de crise responde.
func TestCrisisRoutesRequireToken(t *testing.T) {
	repo, links := newCrisisRepo()
	app := newTestAppWith(crisis.NewService(repo, links))

	for _, path := range []string{"/crises", "/crises/c-1/activate", "/crises/c-1/confirm", "/crises/c-1/close"} {
		status, _ := do(t, app, http.MethodPost, path, "", "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s sem token: status = %d, quero 401", path, status)
		}
	}
}
