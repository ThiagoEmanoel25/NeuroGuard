package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/neuroguard/gateway/internal/domain"
)

// Claims é o "conteúdo" do JWT: os dados que viajam dentro do token.
// Tudo aqui é legível por qualquer um que tenha o token (base64, não criptografia) —
// a assinatura garante que ninguém *alterou*, não que ninguém *leu*.
// Por isso: nunca coloque senha, CPF ou dado clínico aqui.
type Claims struct {
	UserID string      `json:"uid"`
	Role   domain.Role `json:"role"`

	// RegisteredClaims traz os campos padrão da RFC 7519 (exp, iat, sub...).
	// Embutir a struct faz o jwt/v5 validar a expiração automaticamente.
	jwt.RegisteredClaims
}

// Service emite e valida tokens. Não guarda estado de sessão —
// é isso que torna o JWT "stateless" e escalável entre réplicas do gateway.
type Service struct {
	secret []byte
	expiry time.Duration
}

// NewService recebe o segredo e a validade já resolvidos.
// Repare que o pacote auth NÃO importa o pacote config: quem monta a
// dependência é o main.go. Isso mantém auth testável sem variáveis de ambiente.
func NewService(secret string, expiry time.Duration) *Service {
	return &Service{
		secret: []byte(secret),
		expiry: expiry,
	}
}

// GenerateToken cria um access token assinado com HMAC-SHA256.
func (s *Service) GenerateToken(userID string, role domain.Role) (string, error) {
	now := time.Now()

	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry)),
		},
	}

	// NewWithClaims monta o token; SignedString aplica a assinatura com o segredo.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ParseToken valida a assinatura e a expiração, devolvendo os claims.
func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// Checagem de segurança obrigatória: sem ela, um atacante pode trocar o
		// header para alg="none" ou para RS256 e forjar tokens.
		// É o "algorithm confusion attack" — a falha clássica de quem usa JWT.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("algoritmo de assinatura inesperado: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("token inválido")
	}
	return claims, nil
}

// TODO(refresh): quando o client Redis entrar no projeto, adicionar aqui
// GenerateRefreshToken/RotateRefreshToken usando cfg.JWT.RefreshExpiry e
// cfg.Redis — o refresh precisa de estado (para poder ser revogado),
// e é justamente por isso que ele vive no Redis e o access token não.
