# NeuroGuard

Plataforma de monitoramento e resposta a crises epilépticas. O paciente (ou o
wearable) dispara o evento de **aura** — o pródromo que antecede a crise — e o
sistema aciona socorristas e médicos, que confirmam o resgate.

Estado atual: existe o **gateway HTTP** com autenticação completa. O fluxo de
crise ainda é stub (ver [Roadmap](#roadmap)).

## Stack

Go 1.23 · [Fiber v2](https://gofiber.io) · PostgreSQL 16 · JWT (HS256) · bcrypt

## Rodando localmente

Requer Go 1.23+ e Docker.

```bash
# 1. Banco
docker compose up -d

# 2. Variáveis de ambiente
cp .env.example .env     # ajuste se quiser; JWT_SECRET é obrigatório

# 3. Um usuário para entrar (não há endpoint de cadastro ainda)
cd backend/services
go run ./cmd/seed -email ana@exemplo.com -password senha123 -name "Ana" -role patient
go run ./cmd/seed -email bruno@exemplo.com -password senha123 -name "Bruno" -role rescuer

# 4. Gateway
JWT_SECRET=dev-secret-local go run .
```

As migrações rodam automaticamente na subida do gateway (e no seed), em
transação, registradas na tabela `schema_migrations`.

```bash
go build ./... && go vet ./... && go test ./...
```

## API

| Método | Rota              | Proteção                               | Resposta |
|--------|-------------------|----------------------------------------|----------|
| POST   | `/auth/login`     | pública, 10 req/min por IP             | `200` `{token, role}` |
| POST   | `/auth/logout`    | Bearer token                           | `200` (no-op: JWT é stateless) |
| POST   | `/crisis/aura`    | Bearer token                           | `202` |
| POST   | `/crisis/confirm` | Bearer token + role `rescuer`/`doctor` | `202` |

Papéis: `patient`, `rescuer`, `doctor`, `admin`.

```bash
TOKEN=$(curl -s -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@exemplo.com","password":"senha123"}' | jq -r .token)

curl -X POST localhost:8080/crisis/aura -H "Authorization: Bearer $TOKEN"
```

Erros saem sempre no mesmo formato, via `handler.ErrorHandler`:

```json
{ "error": { "code": 401, "message": "credenciais inválidas" } }
```

## Estrutura

```
backend/services/
  main.go                  wiring: config → banco → migrações → repos → rotas
  config/                  configuração via env, falha explícita se faltar algo crítico
  database/                pool pgx + migrações embutidas (//go:embed)
  internal/domain/         Role, User — a única fonte do domínio
  auth/                    Service (JWT), middlewares Protect e RequireRole
  users/                   Repository (interface) + PostgresRepository
  handler/                 login/logout, aura/confirm, tratamento de erro
  cmd/seed/                criação de usuário via CLI
```

O gateway é um **monolito modular** por decisão: os pacotes são separados por
fronteira de domínio, com repositórios atrás de interface, de modo que extrair
um serviço depois seja possível — mas sem pagar hoje o custo de 4 deploys e de
latência de rede dentro do caminho crítico de uma emergência.

## Decisões de segurança

Nenhuma destas é acidental — vale ler antes de "simplificar":

- `ParseToken` **exige** `SigningMethodHMAC`, bloqueando o *algorithm confusion
  attack* (`alg: none` ou troca para RS256).
- O login responde **a mesma mensagem** para email inexistente e senha errada, e
  roda bcrypt contra um hash fictício quando o usuário não existe — fecha
  enumeração de usuários e o vazamento por diferença de tempo.
- O JWT carrega só `uid` e `role`. Nunca senha, CPF ou dado clínico: o payload é
  base64, legível por quem tiver o token. A assinatura garante integridade, não sigilo.
- `401` = não sei quem você é · `403` = sei, mas você não pode.
- `JWT_SECRET` não tem default: a aplicação se recusa a subir sem ele.

## Roadmap

- [x] **Fase 0** — ambiente: Docker Compose, seed de usuário, rate limit no login
- [ ] **Fase 1** — persistir a crise: tabelas `crises` e `care_links`, máquina de
      estados (`aura → ativa → resgate_confirmado → encerrada`), idempotência
- [ ] **Fase 2** — notificação dos contatos vinculados ao paciente
- [ ] **Fase 3** — histórico clínico e autorização **por vínculo**, não só por papel
      (hoje qualquer médico poderia confirmar o resgate de qualquer paciente)
- [ ] **Fase 4+** — telemetria do wearable, predição por ML, refresh token no Redis
