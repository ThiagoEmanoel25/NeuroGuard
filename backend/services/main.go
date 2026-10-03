package main

import (
	"context"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/config"
	"github.com/neuroguard/gateway/database"
	"github.com/neuroguard/gateway/handler"
	"github.com/neuroguard/gateway/internal/domain"
	"github.com/neuroguard/gateway/users"
)

func main() {
	// Fonte única da verdade para configuração — falha aqui se algo crítico faltar.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("erro ao carregar configuração: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.Database.URL)
	if err != nil {
		log.Fatalf("erro ao conectar ao PostgreSQL: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatalf("erro ao aplicar migrações do banco: %v", err)
	}
	userRepo := users.NewPostgresRepository(db)

	app := fiber.New(fiber.Config{
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		ErrorHandler: handler.ErrorHandler,
	})

	// Authentication middleware
	authSvc := auth.NewService(
		cfg.JWT.Secret,
		time.Duration(cfg.JWT.ExpiryHours)*time.Hour,
	)

	// Rate limit só no login: é o único endpoint sem token, logo o único
	// alvo de força bruta. Chave é o IP (padrão do limiter).
	// ponytail: em memória — vira Redis quando houver mais de uma réplica,
	// porque hoje cada réplica conta separado.
	loginLimiter := limiter.New(limiter.Config{
		Max:        10,
		Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "muitas tentativas de login — aguarde um minuto")
		},
	})

	app.Post("/auth/login", loginLimiter, handler.Login(authSvc, userRepo))
	app.Post("/auth/logout", authSvc.Protect(handler.Logout(authSvc)))

	app.Post("/crisis/aura", authSvc.Protect(handler.TriggerAura))

	// Confirmar resgate é ação de socorrista/médico — paciente não pode.
	app.Post("/crisis/confirm", authSvc.Protect(
		auth.RequireRole(handler.ConfirmRescue, domain.RoleRescuer, domain.RoleDoctor),
	))

	log.Printf("gateway ouvindo na porta %s (env=%s)", cfg.Server.Port, cfg.Server.Env)
	log.Fatal(app.Listen(":" + cfg.Server.Port))
}
