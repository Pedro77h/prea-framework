# Preá Framework

Framework web modular para Go, inspirado no Spring e no Angular. Ele traz módulos com injeção de dependência por tag, handlers HTTP sobre o [gin](https://github.com/gin-gonic/gin), um repositório base para Postgres com queries no estilo PDO, middlewares em três níveis e uma CLI que gera o código de cada módulo.

> **Status:** em desenvolvimento (pré-v0). A API ainda muda. Os gaps conhecidos e os próximos passos estão no [ROADMAP.md](ROADMAP.md).

## Sumário

- [Requisitos](#requisitos)
- [Começando](#começando)
- [Estrutura do projeto](#estrutura-do-projeto)
- [Conceitos](#conceitos)
  - [Módulos (preás)](#módulos-preás)
  - [Injeção de dependência](#injeção-de-dependência)
  - [Handler, Service, Repository, Model e DTOs](#handler-service-repository-model-e-dtos)
  - [Banco de dados](#banco-de-dados)
  - [Middlewares](#middlewares)
  - [Erros e respostas](#erros-e-respostas)
  - [Autenticação](#autenticação)
- [CLI](#cli)
- [Configuração](#configuração)
- [Testes](#testes)

## Requisitos

- Go **1.26+**
- PostgreSQL (com `gen_random_uuid()`, nativo a partir do 13)

## Começando

```bash
# 1. CLI
go install ./cmd/prea

# 2. Configuração
cp .env.example .env        # preencha PGCONN e JWTKEY (use um segredo longo)

# 3. Banco: por enquanto as migrations são aplicadas manualmente
psql "$PGCONN" -f internal/preas/user/migrations/user.sql

# 4. Subir o servidor
prea run -p 8000            # equivale a: PORT=8000 go run ./cmd/server
```

Para testar:

```bash
# cadastro (rota pública)
curl -X POST localhost:8000/users -H 'Content-Type: application/json' \
  -d '{"name":"Pedro","username":"pedro","email":"p@x.com","password":"s3cret","profile":1}'

# login → { "token": "...", "payload": {...} }
curl -X POST localhost:8000/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"pedro","password":"s3cret"}'

# rota protegida
curl localhost:8000/users -H "Authorization: Bearer <token>"
```

## Estrutura do projeto

```
prea/
├── cmd/
│   ├── prea/                # CLI (instalável; não depende do projeto)
│   │   └── templates/       # templates embutidos no binário
│   └── server/              # entrypoint da aplicação
├── internal/                # código da aplicação
│   ├── preas/
│   │   ├── preas.go         # lista de módulos e middlewares globais
│   │   ├── user/
│   │   └── auth/
│   └── middleware/          # middlewares da aplicação
├── core/                    # framework: Module, App, Router, Middleware, respostas
├── di/                      # framework: container de injeção
├── database/                # framework: BaseRepository, Prepare
├── security/                # framework: JWT e hash de senha (argon2id)
└── common/                  # framework: erros de domínio e env
```

Direção das dependências: **CLI → nada**, **app → framework**, **framework → nada da app**.

## Conceitos

### Módulos (preás)

Cada funcionalidade é um módulo em `internal/preas/<nome>/`:

```
user/
├── module.go       # declara os componentes, exports, imports e middlewares
├── handler.go      # HTTP: rotas, leitura do corpo da requisição, respostas
├── service.go      # regras de negócio
├── repository.go   # acesso ao banco
├── model.go        # entidade (mapeia a tabela)
└── dtos/           # um arquivo por DTO
```

```go
var Module = &core.Module{
	Name: "user",
	Providers: []any{
		new(Repository),
		di.Export(new(Service)), // visível para módulos que importarem este
		new(Handler),
	},
}
```

Um módulo que usa outro o declara em `Imports`:

```go
var Module = &core.Module{
	Name:      "auth",
	Imports:   []*core.Module{user.Module},
	Providers: []any{new(Service), new(Handler)},
}
```

Os módulos são ligados à aplicação em `internal/preas/preas.go`. O `prea g p` registra o módulo novo ali automaticamente.

### Injeção de dependência

Campos com a tag `inject:""` são preenchidos na subida:

```go
type Service struct {
	Repo  *Repository   `inject:""`
	Users *user.Service `inject:""` // vem do export de um módulo importado
}
```

- **Ordem de busca:** os providers do próprio módulo, depois os exports dos módulos importados, depois os globais (`core.App.Globals`, por exemplo o pool do banco).
- **Interfaces:** um campo do tipo interface é injetado se existir exatamente um provider que a implementa.
- **Structs embutidas** também são injetadas; é assim que o `BaseRepository` recebe a conexão.
- **Campo já preenchido** à mão é mantido, o que ajuda em testes com mocks.
- Os componentes são **singletons** e precisam ser ponteiros para struct. Os campos injetados precisam ser exportados.
- **Erros aparecem na subida**, com o caminho do campo, por exemplo `di: module auth: Service.Users: no provider for *user.Service`. Isso vale para dependência faltando, provider duplicado, interface ambígua e ciclo de imports.

A tag `inject` sem valor também funciona, mas o `go vet` reclama dela. Prefira `inject:""`.

### Handler, Service, Repository, Model e DTOs

**Handler.** Só cuida de HTTP. Todo componente que implementa `core.Router` é montado automaticamente:

```go
type Handler struct {
	Service *Service `inject:""`
}

var _ core.Router = (*Handler)(nil) // falha na compilação se Register estiver errado

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/users")
	g.POST("", h.create)
	g.GET("", middleware.Auth, h.list)
}
```

**Service.** Guarda as regras de negócio e devolve os erros de domínio (veja [Erros e respostas](#erros-e-respostas)).

**Model.** A struct da tabela, com o método `Table()`:

```go
type User struct {
	ID        string     `json:"id"`
	LastLogin *time.Time `json:"lastlogin" db:"lastlogin"`
}

func (User) Table() string { return "users" }
```

**DTOs.** Ficam em `dtos/`, um arquivo por DTO. Os campos viram colunas no `Create`/`Update` pela mesma regra de nomes do model.

### Banco de dados

O `database.BaseRepository[T]` traz o CRUD pronto:

| Método | O que faz |
|---|---|
| `GetAll(ctx)` | todas as linhas, ordenadas por `id` |
| `GetByID(ctx, id)` | uma linha ou `ErrNotFound` |
| `Where(ctx, database.Args{"owner_id": 1})` | as linhas que batem com o filtro (`nil` vira `IS NULL`) |
| `First(ctx, args)` | a primeira linha que bate com o filtro |
| `Create(ctx, dto)` | insere **todos** os campos do DTO e devolve a linha criada |
| `Update(ctx, id, dto)` | atualiza só os campos **não zerados**; para gravar um zero, use `database.Args` |
| `Delete(ctx, id)` | apaga a linha, ou devolve `ErrNotFound` |

**Queries próprias, no estilo PDO**, com parâmetros nomeados `@nome`:

```go
type Repository struct {
	database.BaseRepository[User]
}

func (r *Repository) FindByUsername(ctx context.Context, username string) (User, error) {
	return r.Prepare("select * from users where username = @username").
		Bind("username", username).
		One(ctx)
}
```

- `All(ctx)`, `One(ctx)` e `Exec(ctx)` correspondem a fetchAll, fetch e rowCount.
- `database.Prepare[T](conn, sql)` mapeia o resultado para qualquer tipo: um DTO com parte das colunas, ou um valor simples como `Prepare[int64](pool, "select count(*) from users")`.
- `conn` pode ser o pool ou uma transação (`pgx.Tx`).

**Nomes das colunas:** o campo vira snake_case (`OwnerID` → `owner_id`). A tag `db:"nome"` sobrescreve e `db:"-"` ignora o campo. Nomes de tabelas e colunas são sempre escapados.

**Erros do Postgres viram erros do framework:**

| Erro do Postgres | Erro do framework | HTTP |
|---|---|---|
| unique violation | `ErrConflict` | 409 |
| valor malformado (ex.: uuid inválido), not null, foreign key, check | `ErrInvalidInput` | 400 |
| nenhuma linha | `ErrNotFound` | 404 |

### Middlewares

Middleware é um `core.Middleware`, que é o mesmo que `gin.HandlerFunc`, e pode ser usado em três níveis:

```go
// global: internal/preas/preas.go
var Middlewares = []core.Middleware{gin.Logger(), gin.Recovery()}

// por módulo: vale para todas as rotas do módulo
var Module = &core.Module{Name: "admin", Middlewares: []core.Middleware{middleware.Auth}, ...}

// por rota
g.GET("", middleware.Auth, h.list)
```

A ordem de execução é **global → módulo → rota → handler**. Chame `c.Next()` para continuar ou `c.Abort()` para parar a cadeia. Para criar um, use `prea g m <Nome>`.

### Erros e respostas

O service devolve os erros de `common`, com contexto via `fmt.Errorf("%w: ...")`, e o handler chama `core.Error(c, err)`:

| Erro | HTTP |
|---|---|
| `common.ErrInvalidInput` | 400 |
| `common.ErrUnauthorized` | 401 |
| `common.ErrNotFound` | 404 |
| `common.ErrConflict` | 409 |
| qualquer outro | 500, com mensagem genérica; o erro real vai para o log |

Os erros chegam ao cliente como `{"text": "...", "code": 2}`.

### Autenticação

| Rota | O que faz |
|---|---|
| `POST /auth/login` | recebe `{username, password}` e devolve `{token, payload}` com um JWT HS256 válido por 1h |
| `POST /auth/validate` | recebe `{token}` e devolve as claims, ou 401 |
| `middleware.Auth` | exige `Authorization: Bearer <token>` e guarda as claims em `c.Get(middleware.ClaimsKey)` |

As senhas são guardadas com hash argon2id.

> Existem gaps de segurança conhecidos (`JWTKEY` vazia aceita, `profile` escolhido no cadastro, falta de autorização). Veja o [ROADMAP.md](ROADMAP.md) antes de usar em produção.

## CLI

| Comando | O que faz |
|---|---|
| `prea run [-p 8000]` | sobe o servidor (`go run ./cmd/server`); a porta também pode vir de `$PORT` |
| `prea g p <Nome>` | cria `internal/preas/<nome>/` (module, handler, service, repository, model e dtos) e registra o módulo em `preas.go` |
| `prea g m <Nome>` | cria `internal/middleware/<nome>.go` |

O `g` é atalho de `generate`, o `p` de `prea` e o `m` de `middleware`. Rode a CLI na raiz do projeto: ela lê o `go.mod` para descobrir o caminho dos imports.

O módulo gerado tem as rotas `/<nome>` (GET, GET `/:id`, POST, PATCH `/:id`, DELETE `/:id`) e usa a tabela `<nome>`, que você precisa criar no banco.

## Configuração

As variáveis podem vir do ambiente ou de um `.env` na raiz (veja o `.env.example`):

| Variável | Uso |
|---|---|
| `PGCONN` | DSN do Postgres, ex.: `postgres://user:pass@localhost:5432/prea` |
| `JWTKEY` | segredo do JWT; **obrigatório na prática**, use 32+ bytes aleatórios |
| `PORT` | porta HTTP (padrão `8000`) |

## Testes

```bash
go test ./...

# inclui o teste de integração do banco (cria e apaga a tabela "widgets")
PREA_TEST_DATABASE_URL="postgres://user:pass@localhost:5432/prea_test?sslmode=disable" go test ./database
```

## Licença

Veja [LICENSE](LICENSE).
