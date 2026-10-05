package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/crisis"
	"github.com/neuroguard/gateway/internal/domain"
	"github.com/neuroguard/gateway/users"
	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepository struct {
	users map[string]*domain.User
	err   error
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	user, ok := r.users[email]
	if !ok {
		return nil, users.ErrNotFound
	}
	return user, nil
}

func newTestApp() *fiber.App {
	repo, links := newCrisisRepo()
	return newTestAppWith(crisis.NewService(repo, links))
}

// newTestAppWith monta o app de teste com as mesmas rotas e middlewares do
// main.go, para o teste exercitar a composição real (Protect + RequireRole +
// service) e não uma montagem paralela que pode divergir.
func newTestAppWith(crisisSvc *crisis.Service) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	svc := auth.NewService("test-secret", time.Hour)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("dev-only"), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	repo := &fakeUserRepository{users: map[string]*domain.User{
		"paciente@neuroguard.dev": {
			ID:           "u-001",
			Email:        "paciente@neuroguard.dev",
			PasswordHash: string(passwordHash),
			Role:         domain.RolePatient,
		},
	}}
	app.Post("/auth/login", Login(svc, repo))
	app.Post("/crises", svc.Protect(auth.RequireRole(TriggerAura(crisisSvc), domain.RolePatient)))
	app.Post("/crises/:id/activate", svc.Protect(ActivateCrisis(crisisSvc)))
	app.Post("/crises/:id/close", svc.Protect(CloseCrisis(crisisSvc)))
	app.Post("/crises/:id/confirm", svc.Protect(
		auth.RequireRole(ConfirmRescue(crisisSvc), domain.RoleRescuer, domain.RoleDoctor),
	))
	return app
}

func TestLoginRejectsWrongPasswordAndUnknownUser(t *testing.T) {
	app := newTestApp()
	tests := []string{
		`{"email":"paciente@neuroguard.dev","password":"senha-errada"}`,
		`{"email":"desconhecido@neuroguard.dev","password":"dev-only"}`,
	}

	for _, body := range tests {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d; want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	}
}

func TestDummyPasswordHashIsValid(t *testing.T) {
	if _, err := bcrypt.Cost([]byte(dummyPasswordHash)); err != nil {
		t.Fatalf("dummyPasswordHash is not a valid bcrypt hash: %v", err)
	}
}

func TestLoginAndProtectedCrisisFlow(t *testing.T) {
	app := newTestApp()
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":" PACIENTE@NEUROGUARD.DEV ","password":"dev-only"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := app.Test(loginReq)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d; want %d", loginResp.StatusCode, http.StatusOK)
	}

	// O token emitido pelo login é testado no serviço; aqui usamos um token
	// determinístico para exercitar as regras da rota.
	svc := auth.NewService("test-secret", time.Hour)
	patientToken, _ := svc.GenerateToken(testPatientID, domain.RolePatient)
	auraReq := httptest.NewRequest(http.MethodPost, "/crises", nil)
	auraReq.Header.Set("Authorization", "Bearer "+patientToken)
	auraResp, err := app.Test(auraReq)
	if err != nil {
		t.Fatalf("aura request failed: %v", err)
	}
	if auraResp.StatusCode != http.StatusAccepted {
		t.Errorf("aura status = %d; want %d", auraResp.StatusCode, http.StatusAccepted)
	}

	confirmReq := httptest.NewRequest(http.MethodPost, "/crises/c-1/confirm", nil)
	confirmReq.Header.Set("Authorization", "Bearer "+patientToken)
	confirmResp, err := app.Test(confirmReq)
	if err != nil {
		t.Fatalf("confirm request failed: %v", err)
	}
	if confirmResp.StatusCode != http.StatusForbidden {
		t.Errorf("confirm status = %d; want %d", confirmResp.StatusCode, http.StatusForbidden)
	}
}

func TestLoginRejectsEmptyCredentials(t *testing.T) {
	app := newTestApp()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"","password":""}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d; want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("content type = %q; want JSON", resp.Header.Get("Content-Type"))
	}
}
