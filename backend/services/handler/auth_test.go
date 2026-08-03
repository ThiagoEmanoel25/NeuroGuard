package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/internal/domain"
)

func newTestApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	svc := auth.NewService("test-secret", time.Hour)
	app.Post("/auth/login", Login(svc))
	app.Post("/crisis/aura", svc.Protect(TriggerAura))
	app.Post("/crisis/confirm", svc.Protect(auth.RequireRole(ConfirmRescue, domain.RoleRescuer, domain.RoleDoctor)))
	return app
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
	patientToken, _ := svc.GenerateToken("u-001", domain.RolePatient)
	auraReq := httptest.NewRequest(http.MethodPost, "/crisis/aura", nil)
	auraReq.Header.Set("Authorization", "Bearer "+patientToken)
	auraResp, err := app.Test(auraReq)
	if err != nil {
		t.Fatalf("aura request failed: %v", err)
	}
	if auraResp.StatusCode != http.StatusAccepted {
		t.Errorf("aura status = %d; want %d", auraResp.StatusCode, http.StatusAccepted)
	}

	confirmReq := httptest.NewRequest(http.MethodPost, "/crisis/confirm", nil)
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
