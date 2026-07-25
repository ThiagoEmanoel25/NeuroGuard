package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/internal/domain"
)

// ContextUserKey é a chave sob a qual os claims ficam guardados no contexto
// da requisição. Constante para não errar a string espalhada pelos handlers.
const ContextUserKey = "user"

// Protect é um middleware: recebe um handler e devolve outro handler que
// executa o original *somente se* o token for válido.
// Esse padrão (função que embrulha função) é como middlewares funcionam em Go.
func (s *Service) Protect(next fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "header Authorization ausente")
		}

		// Formato esperado pela RFC 6750: "Bearer <token>".
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return fiber.NewError(fiber.StatusUnauthorized, "formato esperado: Bearer <token>")
		}

		claims, err := s.ParseToken(parts[1])
		if err != nil {
			// Mensagem genérica de propósito: detalhar o motivo ("expirado" vs
			// "assinatura inválida") ajuda o atacante a calibrar o ataque.
			return fiber.NewError(fiber.StatusUnauthorized, "token inválido ou expirado")
		}

		// Locals carrega dados pela cadeia da requisição — é como o handler
		// seguinte descobre quem é o usuário sem reprocessar o token.
		c.Locals(ContextUserKey, claims)

		return next(c)
	}
}

// RequireRole restringe a rota a papéis específicos (autorização, não autenticação).
// Use encadeado depois de Protect: s.Protect(auth.RequireRole(handler.X, domain.RoleDoctor)).
func RequireRole(next fiber.Handler, allowed ...domain.Role) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals(ContextUserKey).(*Claims)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "usuário não autenticado")
		}

		for _, role := range allowed {
			if claims.Role == role {
				return next(c)
			}
		}

		// 403 e não 401: o usuário *é* quem diz ser, apenas não pode fazer isso.
		return fiber.NewError(fiber.StatusForbidden, "permissão insuficiente")
	}
}
