package main

import (
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/config"
	"github.com/neuroguard/gateway/handler"
	"github.com/neuroguard/gateway/internal/domain"
)

func main() {
	// Fonte única da verdade para configuração — falha aqui se algo crítico faltar.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("erro ao carregar configuração: %v", err)
	}

	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	})

	// Authentication middleware
	authSvc := auth.NewService(
		cfg.JWT.Secret,
		time.Duration(cfg.JWT.ExpiryHours)*time.Hour,
	)

	app.Post("/auth/login", handler.Login(authSvc))
	app.Post("/auth/logout", handler.Logout(authSvc))

	app.Post("/crisis/aura", authSvc.Protect(handler.TriggerAura))

	// Confirmar resgate é ação de socorrista/médico — paciente não pode.
	app.Post("/crisis/confirm", authSvc.Protect(
		auth.RequireRole(handler.ConfirmRescue, domain.RoleRescuer, domain.RoleDoctor),
	))

	log.Printf("gateway ouvindo na porta %s (env=%s)", cfg.Server.Port, cfg.Server.Env)
	log.Fatal(app.Listen(":" + cfg.Server.Port))
}
