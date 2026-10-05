package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/crisis"
)

// crisisError traduz erro de domínio em HTTP. A tradução mora aqui, e não no
// service: o service não sabe que existe HTTP.
func crisisError(err error) error {
	switch {
	case errors.Is(err, crisis.ErrNotFound):
		// 404 e não 403 inclusive quando falta vínculo: 403 confirmaria a
		// existência de uma crise que o ator não deveria nem saber que existe.
		return fiber.NewError(fiber.StatusNotFound, "crise não encontrada")
	case errors.Is(err, crisis.ErrConflict):
		return fiber.NewError(fiber.StatusConflict, "a crise não está no estado necessário para esta operação")
	case errors.Is(err, crisis.ErrInvalidCloseReason):
		return fiber.NewError(fiber.StatusBadRequest, "motivo inválido: use resolved ou false_alarm")
	default:
		// Erro inesperado não vaza detalhe: o ErrorHandler responde genérico.
		return err
	}
}

// claimsOf recupera a identidade que o Protect guardou. O ", ok" evita panic
// se a rota for registrada por engano sem o middleware.
func claimsOf(c *fiber.Ctx) (*auth.Claims, error) {
	claims, ok := c.Locals(auth.ContextUserKey).(*auth.Claims)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "usuário não autenticado")
	}
	return claims, nil
}

// TriggerAura registra o início de uma aura — o pródromo que antecede a crise.
func TriggerAura(svc *crisis.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := claimsOf(c)
		if err != nil {
			return err
		}

		// Chave de idempotência opcional: protege o replay do wearable, que
		// pode reenviar o evento quando recupera o sinal. Validada aqui porque
		// a coluna é uuid — string solta viraria erro de sintaxe do driver.
		var key *string
		if raw := c.Get("Idempotency-Key"); raw != "" {
			if _, err := uuid.Parse(raw); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "Idempotency-Key deve ser um UUID")
			}
			key = &raw
		}

		// O paciente é quem está no token, nunca o que vem no corpo: senão
		// qualquer paciente dispararia aura em nome de outro.
		cr, created, err := svc.Trigger(c.UserContext(), claims.UserID, key)
		if err != nil {
			return crisisError(err)
		}

		// 202 nos dois casos (criada ou já aberta) para o wearable não precisar
		// ramificar por status code — o corpo diz o que aconteceu.
		// Agora o 202 é honesto: a crise está gravada antes da resposta sair.
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"crisis_id":    cr.ID,
			"status":       cr.Status,
			"already_open": !created,
		})
	}
}

// ActivateCrisis marca que a crise começou (aura -> ativa).
func ActivateCrisis(svc *crisis.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := claimsOf(c)
		if err != nil {
			return err
		}
		if err := svc.Activate(c.UserContext(), claims.UserID, c.Params("id")); err != nil {
			return crisisError(err)
		}
		return c.JSON(fiber.Map{"status": "ativa"})
	}
}

// ConfirmRescue registra que um socorrista assumiu o atendimento.
// O RequireRole da rota garante o papel; o service garante o vínculo com
// este paciente.
func ConfirmRescue(svc *crisis.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := claimsOf(c)
		if err != nil {
			return err
		}
		if err := svc.Confirm(c.UserContext(), claims.UserID, c.Params("id")); err != nil {
			return crisisError(err)
		}
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"confirmed": true,
			"rescuer":   claims.UserID,
		})
	}
}

type closeRequest struct {
	Reason string `json:"reason"`
}

// CloseCrisis encerra a crise. Paciente ou cuidador com vínculo ativo.
func CloseCrisis(svc *crisis.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := claimsOf(c)
		if err != nil {
			return err
		}

		// Corpo é opcional: encerrar sem dizer nada é o caso comum.
		req := closeRequest{Reason: "resolved"}
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&req); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "JSON inválido")
			}
		}

		if err := svc.Close(c.UserContext(), claims.UserID, c.Params("id"), req.Reason); err != nil {
			return crisisError(err)
		}
		return c.JSON(fiber.Map{"status": "encerrada", "reason": req.Reason})
	}
}
