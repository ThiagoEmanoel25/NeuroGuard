package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
)

// TriggerAura e ConfirmRescue têm a assinatura crua de fiber.Handler
// (func(*fiber.Ctx) error), sem construtor, porque ainda não dependem de nada.
// Quando o client gRPC do crisis-service existir, elas viram
// TriggerAura(client crisis.Client) fiber.Handler, igual ao Login.

// TriggerAura registra o início de uma aura (o pródromo que antecede a crise).
func TriggerAura(c *fiber.Ctx) error {
	// Protect já validou o token e guardou os claims em Locals.
	// O type assertion com ", ok" evita panic se a rota for registrada
	// por engano sem o middleware.
	claims, ok := c.Locals(auth.ContextUserKey).(*auth.Claims)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "usuário não autenticado")
	}

	// TODO(crisis): chamar o crisis-service via gRPC em cfg.Services.CrisisServiceURL.
	// 202 Accepted (e não 200) porque o processamento real será assíncrono:
	// o gateway aceita o evento e devolve na hora, sem esperar o pipeline.
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"message": "aura registrada",
		"user_id": claims.UserID,
	})
}

// ConfirmRescue confirma que um socorrista assumiu o atendimento.
func ConfirmRescue(c *fiber.Ctx) error {
	claims, ok := c.Locals(auth.ContextUserKey).(*auth.Claims)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "usuário não autenticado")
	}

	// TODO(crisis): encaminhar ao crisis-service e notificar o paciente.
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"message":   "resgate confirmado",
		"rescuer":   claims.UserID,
		"confirmed": true,
	})
}
