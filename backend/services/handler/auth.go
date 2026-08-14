package handler

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/neuroguard/gateway/auth"
	"github.com/neuroguard/gateway/internal/domain"
	"github.com/neuroguard/gateway/users"
	"golang.org/x/crypto/bcrypt"
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

// Executar bcrypt mesmo quando o usuario nao existe reduz a diferenca de tempo
// entre uma senha errada e um email desconhecido, dificultando enumeracao.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Login recebe o Service e o repositorio por parametro e devolve o handler pronto.
// Esse padrão é injeção de dependência: o handler não cria o auth.Service nem
// lê variáveis de ambiente — recebe pronto, o que o torna testável.
func Login(svc *auth.Service, repo users.Repository) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req loginRequest
		if err := c.BodyParser(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "JSON inválido")
		}
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" || req.Password == "" {
			return fiber.NewError(fiber.StatusBadRequest, "email e senha são obrigatórios")
		}

		u, err := repo.FindByEmail(c.UserContext(), req.Email)
		if errors.Is(err, users.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(req.Password))
			return fiber.NewError(fiber.StatusUnauthorized, "credenciais inválidas")
		}
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "falha ao autenticar usuário")
		}

		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
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
