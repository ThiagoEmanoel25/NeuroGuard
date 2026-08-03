package handler

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/internal/domain"
)

// Structs de request/response ficam minúsculas (não exportadas): elas são
// detalhe interno do transporte HTTP, não fazem parte do domínio.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string      `json:"token"`
	Role  domain.Role `json:"role"`
}

// demoUsers é um substituto temporário do banco de dados.
// ATENÇÃO: senha em texto puro, apenas para destravar o desenvolvimento local.
// TODO(auth): trocar por consulta ao banco + bcrypt (golang.org/x/crypto/bcrypt)
// antes de qualquer deploy. Nunca compare senha com ==.
var demoUsers = map[string]struct {
	ID       string
	Password string
	Role     domain.Role
}{
	"paciente@neuroguard.dev": {ID: "u-001", Password: "dev-only", Role: domain.RolePatient},
	"resgate@neuroguard.dev":  {ID: "u-002", Password: "dev-only", Role: domain.RoleRescuer},
	"medico@neuroguard.dev":   {ID: "u-003", Password: "dev-only", Role: domain.RoleDoctor},
}

// Login recebe o Service por parâmetro e devolve o handler já "amarrado" a ele.
// Esse padrão é injeção de dependência: o handler não cria o auth.Service nem
// lê variáveis de ambiente — recebe pronto, o que o torna testável.
func Login(svc *auth.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req loginRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "JSON inválido")
		}
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.Password == "" {
			return fiber.NewError(fiber.StatusBadRequest, "email e senha são obrigatórios")
		}

		u, ok := demoUsers[req.Email]
		if !ok || u.Password != req.Password {
			// Mesma resposta para "email não existe" e "senha errada".
			// Diferenciar as duas entrega ao atacante a lista de emails válidos
			// (user enumeration) — relevante num app de saúde.
			return fiber.NewError(fiber.StatusUnauthorized, "credenciais inválidas")
		}

		token, err := svc.GenerateToken(u.ID, u.Role)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "falha ao gerar token")
		}

		return c.JSON(loginResponse{Token: token, Role: u.Role})
	}
}

// Logout existe por completude da API, mas hoje é praticamente um no-op.
// Motivo: JWT é stateless — o servidor não guarda sessão, então não há o que
// "apagar". O token continua válido até expirar, mesmo depois deste endpoint.
// TODO(refresh): quando o Redis entrar, o logout de verdade vira duas coisas:
//  1. revogar o refresh token no Redis;
//  2. opcionalmente, colocar o jti do access token numa denylist até o exp.
func Logout(svc *auth.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"message": "logout efetuado — descarte o token no client",
		})
	}
}
