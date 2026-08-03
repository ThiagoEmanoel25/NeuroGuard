package auth

import (
	"testing"
	"time"

	"github.com/neuroguard/gateway/internal/domain"
)

func TestServiceGenerateAndParseToken(t *testing.T) {
	svc := NewService("test-secret", time.Hour)

	token, err := svc.GenerateToken("user-123", domain.RoleDoctor)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if claims.UserID != "user-123" || claims.Subject != "user-123" {
		t.Errorf("user id = %q, subject = %q; want user-123", claims.UserID, claims.Subject)
	}
	if claims.Role != domain.RoleDoctor {
		t.Errorf("role = %q; want %q", claims.Role, domain.RoleDoctor)
	}
}

func TestServiceRejectsExpiredOrForeignToken(t *testing.T) {
	expired := NewService("test-secret", -time.Hour)
	token, err := expired.GenerateToken("user-123", domain.RolePatient)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if _, err := expired.ParseToken(token); err == nil {
		t.Fatal("ParseToken() accepted an expired token")
	}

	issuer := NewService("issuer-secret", time.Hour)
	token, err = issuer.GenerateToken("user-123", domain.RolePatient)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if _, err := NewService("other-secret", time.Hour).ParseToken(token); err == nil {
		t.Fatal("ParseToken() accepted a token signed with another secret")
	}
}
