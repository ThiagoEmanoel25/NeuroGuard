# NeuroGuard

Plataforma de monitoramento e resposta a crises epilépticas. O paciente (ou o
wearable) dispara o evento de **aura** — o pródromo que antecede a crise — e o
sistema aciona socorristas e médicos, que confirmam o resgate.

Estado atual: o **gateway HTTP** tem autenticação completa e o fluxo de crise
persistido — aura, início, confirmação de resgate e encerramento, com
idempotência e autorização por vínculo.

## Stack

Go 1.23 · [Fiber v2](https://gofiber.io) · PostgreSQL 16 · JWT (HS256) · bcrypt

## Rodando localmente

Requer Go 1.23+ e Docker.

```bash
# 1. Banco
docker compose up -d

# 2. Variáveis de ambiente
cp .env.example .env     # ajuste se quiser; JWT_SECRET é obrigatório

# 3. Usuários e vínculo (não há endpoint de cadastro nem de convite ainda)
cd backend/services
go run ./cmd/seed -email ana@exemplo.com -password senha123 -name "Ana" -role patient
go run ./cmd/seed -email bruno@exemplo.com -password senha123 -name "Bruno" -role rescuer

# Sem vínculo, nenhum socorrista pode confirmar o resgate de Ana.
go run ./cmd/seedlink -patient ana@exemplo.com -caregiver bruno@exemplo.com

# 4. Gateway
JWT_SECRET=dev-secret-local go run .
```

As migrações rodam automaticamente na subida do gateway (e no seed), em
transação, registradas na tabela `schema_migrations`.

```bash
go build ./... && go vet ./... && go test ./...

# Os testes de concorrência do pacote crisis precisam de Postgres de verdade:
# sem DATABASE_URL eles pulam (e a suíte segue verde).
DATABASE_URL='postgres://postgres:postgres@localhost:5432/neuroguard?sslmode=disable' \
  go test -race ./...
```

## API

| Método | Rota                     | Proteção | Resposta |
|--------|--------------------------|----------|----------|
| POST   | `/auth/login`            | pública, 10 req/min por IP | `200` `{token, role}` |
| POST   | `/auth/logout`           | token | `200` (no-op: JWT é stateless) |
| POST   | `/crises`                | token + role `patient` | `202` `{crisis_id, status, already_open}` |
| POST   | `/crises/:id/activate`   | token + paciente ou cuidador vinculado | `200` |
| POST   | `/crises/:id/confirm`    | token + role `rescuer`/`doctor` + vínculo ativo | `202` |
| POST   | `/crises/:id/close`      | token + paciente ou cuidador vinculado | `200` |

Papéis: `patient`, `rescuer`, `doctor`, `admin`.

**Fases da crise:** `aura → ativa → encerrada`, mais `aura → encerrada` para
alarme falso. O **resgate não é fase**: é eixo ortogonal (`rescuer_id`,
`rescue_confirmed_at`), porque o socorrista é avisado na aura e pode confirmar
antes de a crise começar.

`POST /crises` é idempotente de duas formas: um paciente só tem uma crise
aberta por vez (índice único parcial), e o header opcional `Idempotency-Key`
(UUID) faz um reenvio do wearable devolver a crise original mesmo que ela já
tenha sido encerrada. Nos dois casos a resposta é `202` com `already_open`
dizendo o que aconteceu.

Códigos: `401` sem identidade · `403` papel insuficiente · `404` crise
inexistente **ou** ator sem vínculo · `409` estado incompatível (resgate já
assumido, crise encerrada, transição inexistente) · `400` entrada inválida.

```bash
login() { curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d "{\"email\":\"$1\",\"password\":\"senha123\"}" | jq -r .token; }

ANA=$(login ana@exemplo.com)
BRUNO=$(login bruno@exemplo.com)

# Paciente dispara a aura; o id do paciente sai do token, nunca do corpo.
CID=$(curl -s -X POST localhost:8080/crises -H "Authorization: Bearer $ANA" | jq -r .crisis_id)

# Socorrista vinculado assume o resgate. Sem vínculo, esta chamada dá 404.
curl -X POST localhost:8080/crises/$CID/confirm -H "Authorization: Bearer $BRUNO"

curl -X POST localhost:8080/crises/$CID/close -H "Authorization: Bearer $ANA" \
  -H 'Content-Type: application/json' -d '{"reason":"resolved"}'
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
  crisis/                  Repository, PostgresRepository (CAS) e Service (regras)
  care/                    vínculo paciente<->cuidador (IsActiveLink)
  handler/                 login/logout, rotas de crise, tratamento de erro
  cmd/seed/                criação de usuário via CLI
  cmd/seedlink/            criação de vínculo via CLI
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
- **Autorização por vínculo, não só por papel.** `RequireRole` garante que o ator
  é socorrista ou médico; só o `crisis.Service` garante que ele é socorrista
  *deste* paciente, consultando `care_links`. Sem isso qualquer socorrista
  cadastrado confirmaria qualquer resgate — é a falha nº 1 do OWASP API Top 10
  (BOLA), e aqui vazaria dado de saúde.
- **Falta de vínculo responde `404`, não `403`.** O `403` confirmaria que a crise
  existe. A regra "403 = sei quem você é, mas não pode" vale para papel; para
  vínculo, o ator não deve nem saber que o recurso existe.
- **A autorização vem antes da checagem de estado.** Invertido, um ator sem
  vínculo receberia `409` numa crise encerrada e com isso descobriria que ela
  existe.
- O vínculo é lido do banco, nunca do JWT: o `role` no token fica congelado até
  o `exp`, então um cuidador desvinculado hoje seguiria autorizado até o token
  vencer.
- As invariantes de estado estão no schema (`CHECK`, índices únicos parciais),
  não só no Go: elas valem mesmo com bug na aplicação ou `psql` na mão.

## Roadmap

- [x] **Fase 0** — ambiente: Docker Compose, seed de usuário, rate limit no login
- [x] **Fase 1** — crise persistida: `crises` e `care_links`, fases
      `aura → ativa → encerrada` com resgate ortogonal, idempotência em duas
      camadas, e autorização por vínculo no confirmar/ativar/encerrar
- [ ] **Fase 2** — notificação dos contatos vinculados, por ordem de `priority`
- [ ] **Fase 3** — histórico clínico, com ReBAC também na leitura, e trilha de
      acesso (LGPD art. 37)
- [ ] **Fase 4+** — telemetria do wearable, predição por ML, refresh token no Redis

### Dívidas conhecidas
- Crise esquecida bloqueia a próxima (índice único parcial) — não há timeout
  automático, por decisão: exige política clínica. Encerramento manual existe.
- Sem endpoint de cadastro nem de convite de vínculo: `cmd/seed` e
  `cmd/seedlink` cobrem o desenvolvimento.
- `applyMigration` não tem trava entre processos; com duas réplicas subindo
  juntas a segunda falha. `pg_advisory_xact_lock` resolve quando houver réplicas.
- `GenerateToken` não emite `jti`, que a denylist do `TODO(refresh)` vai exigir.
- `config.ServicesConfig` aponta microsserviços gRPC que não existem.
