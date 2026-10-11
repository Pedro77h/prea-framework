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
  - [Logs](#logs)
  - [Erros e respostas](#erros-e-respostas)
  - [Segurança HTTP](#segurança-http)
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
cp .env.example .env        # preencha PGCONN e JWTKEY (openssl rand -base64 32)

# 3. Banco: por enquanto as migrations são aplicadas manualmente
psql "$PGCONN" -f internal/preas/user/migrations/user.sql

# 4. Subir o servidor
prea run -p 8000            # compila ./cmd/server e roda com PORT=8000
```

Para testar:

```bash
# cadastro (rota pública)
curl -X POST localhost:8000/users -H 'Content-Type: application/json' \
  -d '{"name":"Pedro","username":"pedro","email":"p@x.com","password":"s3cret","profile":1}'

# login → { "code": 1, "data": { "token": "...", "payload": {...} } }
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
│   │   ├── preas.go         # módulos, segurança HTTP e middlewares globais
│   │   ├── user/
│   │   └── auth/
│   └── middleware/          # middlewares da aplicação
├── core/                    # framework: Module, App, Router, Middleware, Security, respostas
├── di/                      # framework: container de injeção
├── database/                # framework: BaseRepository, Prepare
├── security/                # framework: JWT e hash de senha (argon2id)
├── logger/                  # framework: logs por nível (slog)
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
		Repository{},
		di.Export(Service{}), // visível para módulos que importarem este
		Handler{},
	},
}
```

Um módulo que usa outro o declara em `Imports`:

```go
var Module = &core.Module{
	Name:      "auth",
	Imports:   []*core.Module{user.Module},
	Providers: []any{Service{}, Handler{}},
}
```

Os módulos são ligados à aplicação em `internal/preas/preas.go`. O `prea g p` registra o módulo novo ali automaticamente.

### Injeção de dependência

> O guia completo, com a comparação com os beans do Spring, o ciclo de vida, a resolução, os erros e a migração, está em [docs/injecao-de-dependencia.md](docs/injecao-de-dependencia.md).

Campos com a tag `inject:""` são preenchidos na subida:

```go
type Service struct {
	Repo  *Repository   `inject:""`
	Users *user.Service `inject:""` // vem do export de um módulo importado
}
```

- **Ordem de busca:** os providers do próprio módulo, depois os exports dos módulos importados, depois os globais (`core.App.Globals`, por exemplo o pool do banco).
- **Interfaces:** um campo do tipo interface é injetado se existir exatamente um provider que a implementa.
- **Mais de um candidato** para o mesmo tipo dá erro na subida, e o erro pede um nome. Nunca é escolhido um em silêncio.
- **Structs embutidas** também são injetadas; é assim que o `BaseRepository` recebe a conexão.
- **Providers são declarados como valor** (`Repository{}`), e não como ponteiro. O módulo é só uma declaração: cada container (cada `core.App`) cria as **próprias instâncias** a partir dela e nunca altera a declaração.
  - Dois `App` no mesmo processo, como testes com bancos diferentes ou uma API e um admin, não compartilham nada.
  - Vários containers podem carregar o mesmo módulo ao mesmo tempo sem data race.
  - Declarar com `new(Repository)` dá erro na subida: `provider *user.Repository must be declared as a value, user.Repository{}`.
- **Campo preenchido na declaração** é mantido, o que serve para mocks e configuração: `Service{Repo: mockRepo}` e `routes{path: "/x"}`. A cópia é rasa, então ponteiros preenchidos ali são compartilhados por quem usar a mesma declaração.
- Os componentes são **singletons por container**: dentro de um `App`, todos que injetam `*Service` recebem a mesma instância. Os campos injetados precisam ser exportados.
- Instâncias prontas, como o pool do banco, entram em `core.App.Globals`, e essas sim são compartilhadas.
- **Concorrência:** um mesmo componente atende todas as requisições ao mesmo tempo, cada uma em sua goroutine. Não guarde estado de requisição em campos da struct; use variáveis locais ou o `gin.Context`. Estado compartilhado mutável precisa de `sync.Mutex` ou `atomic`.
- **Erros aparecem na subida**, com o caminho do campo, por exemplo `di: module auth: Service.Users: no provider for *user.Service`. Isso vale para dependência faltando, provider duplicado, interface ambígua e ciclo de imports.

Use sempre `inject:""`. A forma curta `inject` sem valor não é uma tag válida em Go (o `go vet` acusa) e dá erro na subida.

**Componentes nomeados.** O nome é opcional, como um token do Angular. Ele serve para quando há mais de um componente do mesmo tipo:

```go
// cmd/server/main.go
Globals: []any{mainDB, di.Named("analytics", analyticsDB)}

// em um módulo; Named e Export podem ser combinados em qualquer ordem
Providers: []any{di.Named("stripe", Gateway{}), di.Export(di.Named("paypal", Gateway{}))}

type Reports struct {
	DB        *pgxpool.Pool `inject:""`          // o sem nome (mainDB)
	Analytics *pgxpool.Pool `inject:"analytics"` // o nomeado
}
```

- `inject:"nome"` procura só os componentes com esse nome.
- `inject:""` prefere o componente sem nome. Se só existir um, mesmo nomeado, ele é usado.

**Ciclo de vida.** Um componente pode ter, opcionalmente, hooks de subida e de desligamento:

```go
type Mailer struct {
	Config *Config `inject:""`
	client *smtp.Client
}

// roda na subida, depois da injeção e antes de o servidor aceitar requisições
func (m *Mailer) Init(ctx context.Context) error {
	c, err := smtp.Dial(m.Config.SMTPAddr)
	if err != nil {
		return err // a app não sobe
	}
	m.client = c
	return nil
}

// roda no desligamento, depois que as requisições em andamento terminam
func (m *Mailer) Close(ctx context.Context) error { return m.client.Quit() }
```

- **`Init`** faz o papel de construtor: lê config, valida, abre conexões. Se der erro, a app não sobe, e os componentes que já tinham iniciado são fechados.
- **Ordem:** o `Init` segue as dependências. Se A injeta B, o `B.Init` roda antes do `A.Init`, inclusive entre módulos. O `Close` roda na ordem inversa.
- **`Close`** pode ser `Close(ctx) error` ou `Close() error` (`io.Closer`). Ele roda no desligamento gracioso, antes de o pool do banco fechar.
- **Globals não passam pelo ciclo de vida:** quem cria, como o `main` com o pool, é quem fecha.
- **Em testes:** `engine, closer, err := app.Build(ctx)` monta a app com o `Init` e devolve o `closer`, que roda o `Close`.

### Handler, Service, Repository, Model e DTOs

**Handler.** Só cuida de HTTP. Todo componente que implementa `core.Router` é montado automaticamente:

```go
type Handler struct {
	Service *Service         `inject:""`
	Auth    *middleware.Auth `inject:""`
}

var _ core.Router = (*Handler)(nil) // falha na compilação se Register estiver errado

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/users")
	g.POST("", h.create)
	g.GET("", h.Auth.Handle, h.list)
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
| `Update(ctx, id, dto)` | *patch*: atualiza só os campos **não zerados**; para gravar um zero, use ponteiro no DTO (`nil` = não mexe) ou `database.Args` |
| `Save(ctx, entidade)` | como o `save` do JPA: grava **todas** as colunas, zeros inclusive. Insere se a PK está zerada, senão atualiza (`ErrNotFound` se não existe) |
| `Count(ctx, args)` | quantas linhas batem com o filtro (`nil` conta todas) |
| `Exists(ctx, args)` | se alguma linha bate com o filtro |
| `Delete(ctx, id)` | apaga a linha, ou devolve `ErrNotFound` |

**Atualizar no estilo JPA:** carregue, altere e salve. Use uma transação quando a leitura e a escrita precisam ser atômicas:

```go
err := s.Transaction(ctx, func(ctx context.Context) error {
	u, err := s.Repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	u.Active = false // um zero que o Update ignoraria
	_, err = s.Repo.Save(ctx, u)
	return err
})
```

No insert, o `Save` grava todos os campos, então só a PK recebe o default do banco. Para usar os defaults das outras colunas, use o `Create` com um DTO.

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
- **Escolher os campos:** os métodos prontos usam `select *`; para trazer só algumas colunas, escreva-as no `Prepare`. `database.Prepare[T](conn, sql)` mapeia o resultado para qualquer tipo: um DTO com parte das colunas (`Prepare[Summary](r.DB, "select id, name from users")`), ou um valor simples como `Prepare[int64](r.DB, "select max(profile) from users")`.
- `conn` pode ser o pool ou uma transação (`pgx.Tx`). Com o pool, a transação do `ctx` (veja abaixo) tem prioridade.

**Transações, no estilo `@Transactional`:** embuta `database.Transactional` no componente e envolva os métodos que precisam ser atômicos:

```go
type Service struct {
	database.Transactional // injeta o pool, como um inject:""
	Users  *Repository `inject:""`
	Audits *audit.Service `inject:""`
}

func (s *Service) Register(ctx context.Context, req dtos.CreateRequest) (User, error) {
	var u User
	err := s.Transaction(ctx, func(ctx context.Context) error {
		var err error
		if u, err = s.Users.Create(ctx, req); err != nil {
			return err
		}
		return s.Audits.Log(ctx, "user created", u.ID)
	})
	return u, err
}
```

- **A transação viaja no `ctx`.** Tudo que roda com o `ctx` recebido pela função usa a transação: o `BaseRepository`, o `r.Prepare` e os services de outros preás. Os repositórios não mudam.
- **Commit** quando a função devolve `nil`. **Rollback** em erro ou panic, e o panic continua subindo.
- **Aninhada**, uma transação dentro de outra abre um savepoint: o erro desfaz só o trabalho interno, e a externa decide se segue ou devolve o erro.
- **Cuidados:**
  - use o `ctx` da função, não o de fora, senão a query roda fora da transação;
  - uma transação é uma conexão, então não use o `ctx` dela em várias goroutines ao mesmo tempo;
  - acesso direto ao pool (`r.DB.Query`) fica fora da transação: use `r.Prepare`.

**Nomes das colunas:** o campo vira snake_case (`OwnerID` → `owner_id`). A tag `db:"nome"` sobrescreve e `db:"-"` ignora o campo. Nomes de tabelas e colunas são sempre escapados.

**Erros do Postgres viram erros do framework:**

| Erro do Postgres | Erro do framework | HTTP |
|---|---|---|
| unique violation | `ErrConflict` | 409 |
| valor malformado (ex.: uuid inválido), not null, foreign key, check | `ErrInvalidInput` | 400 |
| nenhuma linha | `ErrNotFound` | 404 |

### Middlewares

Existem dois tipos de middleware:

- **Componente** (padrão do `prea g m <Nome>`): uma struct com campos `inject` e um método `Handle(c *gin.Context)`. O container gerencia como qualquer componente: cria, injeta e chama `Init`/`Close`. Use quando o middleware precisa de um service, do banco ou de config.
- **Função** (`prea g m <Nome> --func`): um `core.Middleware`, que é o mesmo que `gin.HandlerFunc`, sem injeção.

```go
// componente: internal/middleware/auth.go
type Auth struct {
	Sessions Sessions `inject:""` // interface; user.Service a implementa
}

func (a *Auth) Handle(c *gin.Context) { ... }
```

Onde cada um pode ser usado:

| Nível | Função | Componente |
|---|---|---|
| **global** (`preas.Middlewares` e `Security.Middlewares`) | `core.RequestLogger` | `middleware.RateLimit{}` |
| **módulo** (`Module.Middlewares`, vale para todas as rotas do módulo) | `gin.BasicAuth(contas)` | `middleware.Auth{}` |
| **rota** | `g.GET("", middleware.Owner, h.list)` | injetado no handler: `g.GET("", h.Auth.Handle, h.list)` |

```go
var Module = &core.Module{
	Name:        "admin",
	Providers:   []any{Handler{}},
	Middlewares: []any{core.Middleware(audit), middleware.Auth{}}, // funções e componentes, na ordem
}
```

- **Por módulo**, o componente é criado automaticamente: não precisa repetir em `Providers`.
- **Global**, o componente fica num módulo implícito `app` que importa todos os módulos: injeta os `Globals` e os exports (`di.Export`) de qualquer módulo, e passa por `Init`/`Close`. Se aparece nas duas listas globais, é a mesma instância.
- **Por rota**, declare `middleware.Auth{}` em `Providers` e injete no handler (``Auth *middleware.Auth `inject:""` ``).
- **Nos dois ao mesmo tempo**, é a mesma instância.
- **Erros na subida, não em produção:** declarar `&middleware.Auth{}` ou uma struct sem `Handle`.
- **Cuidado com o atalho** `(&middleware.Auth{}).Handle`. Ele compila, mas cria um `Auth` fora do container e sem injeção, e dá panic na primeira requisição. Passe sempre o valor `middleware.Auth{}`.

Para depender de outro preá sem ciclo de imports, use uma interface, como `Sessions` no exemplo.

A ordem de execução é **segurança → global → módulo → rota → handler**. Chame `c.Next()` para continuar ou `c.Abort()` para parar a cadeia.

### Logs

O pacote `logger` tem quatro níveis, em ordem: **debug < info < success < error**.

```go
logger.Debug("cache miss", "key", k)
logger.Info("enviando e-mail", "to", u.Email)
logger.Success("usuário criado", "id", u.ID)
logger.Error("falha no pagamento", "error", err)
```

```
time=2026-10-10T03:09:47.989-03:00 level=SUCCESS msg=request method=POST path=/users status=201 latency=18.6ms ip=::1
```

- **Argumentos** são pares chave/valor. A saída é estruturada (`log/slog`), em texto ou JSON.
- **`LOG_LEVEL`** define o nível mínimo, e o padrão é `info`. Com `debug`, aparece também a saída do gin (rotas registradas, avisos).
- **`LOG_FORMAT=json`** é a opção para produção ou para coletores de log.
- **`logger.Log`** é o `*slog.Logger` por trás e também o default do `slog`. Assim, o `log.Printf` e o `slog.Info` de qualquer lib caem no mesmo lugar. Para um logger próprio, como em testes ou outro destino, use `logger.New(w, level, json)`.
- **`core.RequestLogger`** gera uma linha por requisição: **success** abaixo de 400, **info** para 4xx e **error** para 5xx. Numa linha de 500 vem junto o erro real que foi passado ao `core.Error`; o cliente só vê "internal server error".
- **`core.Recovery`** transforma um panic em 500 e loga o erro com a stack trace.

### Erros e respostas

O service devolve os erros de `common`, com contexto via `fmt.Errorf("%w: ...")`, e o handler chama `core.Error(c, err)`:

| Erro | HTTP |
|---|---|
| `common.ErrInvalidInput` | 400 |
| `common.ErrUnauthorized` | 401 |
| `common.ErrForbidden` | 403 |
| `common.ErrNotFound` | 404 |
| `common.ErrConflict` | 409 |
| qualquer outro | 500, com mensagem genérica; o erro real vai para o log |

Os erros chegam ao cliente como `{"text": "...", "code": 2}`.

**Respostas.** Todas usam o mesmo envelope `{"text", "code", "data"}`, montado com qualquer combinação de partes. Go não tem overload de métodos; o equivalente aqui é uma função que aceita partes variádicas:

```go
core.Reply(c, 200, core.Text("salvo"))                           // {"text":"salvo","code":1}
core.Reply(c, 200, core.Code(core.WarningCode))                  // {"code":2}
core.Reply(c, 200, core.Text("salvo"), core.Code(core.WarningCode))
core.Reply(c, 201, core.Text("criado"), core.Data(user))         // {"text":"criado","code":1,"data":{...}}
core.Reply(c, 200, core.Data(users))                             // {"code":1,"data":[...]}

core.Response(c, 200, qualquerCoisa)                             // formato próprio, sem envelope
```

Sem `Code`, ele vem do status HTTP: abaixo de 400 é `SuccessCode` (1), abaixo de 500 é `WarningCode` (2) e o resto é `DangerCode` (3). O JSON sai indentado só em modo debug.

### Servidor

Os timeouts ficam em `internal/preas/preas.go`. Um campo zerado usa o padrão:

```go
var Server = core.Server{
	ReadHeaderTimeout: 5 * time.Second,   // padrão 5s (proteção contra slowloris)
	ReadTimeout:       30 * time.Second,  // padrão 30s, corpo incluído
	WriteTimeout:      30 * time.Second,  // padrão 30s; aumente para streaming e downloads longos
	IdleTimeout:       120 * time.Second, // padrão 120s, conexões keep-alive
	ShutdownTimeout:   15 * time.Second,  // padrão 15s, tempo para as requisições em andamento terminarem
	MaxHeaderBytes:    1 << 20,           // padrão 1 MB
}
```

**Subida e desligamento:**
1. Na subida, o banco recebe um `Ping`, então um DSN errado falha na hora, e não na primeira requisição.
2. Em SIGINT/SIGTERM, o servidor para de aceitar conexões e espera as requisições em andamento por até `ShutdownTimeout`.
3. Só depois o pool do banco é fechado. Nenhuma query em andamento perde a conexão.
4. Se o tempo acaba, as conexões são fechadas e o contexto das requisições é cancelado. Por isso as queries devem usar `c.Request.Context()`.
5. Um segundo Ctrl+C encerra na hora.

O tamanho do pool de conexões vai no próprio DSN: `?pool_max_conns=20` (o padrão é o maior entre 4 e o número de CPUs).

### Segurança HTTP

A configuração fica em `internal/preas/preas.go` e roda antes de qualquer middleware:

```go
var Security = core.Security{
	CORS: &core.CORS{
		AllowOrigins: []string{"http://localhost:3000"}, // "*" libera qualquer origem
		// AllowMethods, AllowHeaders, ExposeHeaders, AllowCredentials, MaxAge
	},
	CSRF: &core.CSRF{
		// TrustedOrigins: []string{"https://admin.exemplo.com"},
		// Bypass:         []string{"POST /webhooks/"},
	},
	Headers: core.DefaultHeaders, // nosniff, X-Frame-Options, CSP, HSTS...
	// TrustedProxies: []string{"10.0.0.0/8"},
}
```

| Campo | O que faz | `nil` |
|---|---|---|
| `CORS` | responde o preflight (`OPTIONS`) e coloca os headers `Access-Control-*` nas origens permitidas | sem CORS: o navegador bloqueia chamadas de outras origens |
| `CSRF` | bloqueia com 403 requisições não seguras (POST, PATCH...) vindas de outra origem pelo navegador, usando o `net/http.CrossOriginProtection`; as origens do CORS já são confiáveis; curl e chamadas servidor a servidor passam | sem proteção |
| `Headers` | headers colocados em toda resposta; use `maps.Clone(core.DefaultHeaders)` para ajustar | nenhum |
| `TrustedProxies` | proxies cujo `X-Forwarded-For` é aceito no `c.ClientIP()` | nenhum proxy é confiável |

`AllowCredentials` com origem `"*"` é recusado na subida.

**Configurações próprias.** Para casos que o framework não cobre:

```go
var Security = core.Security{
	// regra própria de origem (subdomínios, consulta ao banco...); o CSRF também confia nela
	CORS: &core.CORS{AllowOriginFunc: func(o string) bool { return strings.HasSuffix(o, ".exemplo.com") }},

	// resposta própria quando o CSRF bloqueia (precisa chamar c.Abort)
	CSRF: &core.CSRF{Deny: func(c *gin.Context) { c.AbortWithStatusJSON(403, gin.H{"erro": "origem"}) }},

	// suas proteções: rodam depois de Headers, CORS e CSRF e antes dos middlewares globais
	Middlewares: []any{middleware.RateLimit{}, middleware.CSRFToken}, // componentes e funções
}
```

- **Peças avulsas:** `core.Headers(map)`, `(&core.CORS{...}).Middleware()` e `(&core.CSRF{...}).Middleware()` funcionam por módulo ou por rota, por exemplo um CORS diferente só em `/public`.
- **Desligar uma peça:** deixe o campo `nil` e coloque a sua versão em `Middlewares`, ou use outra lib, como o `gin-contrib/cors`.

### Autenticação

| Rota | O que faz |
|---|---|
| `POST /auth/login` | recebe `{username, password}` e devolve `{token, payload}` com um JWT HS256 válido por 1h |
| `POST /auth/validate` | recebe `{token}` e devolve as claims, ou 401 |
| `POST /auth/logout` | encerra a sessão: o token deixa de valer (204) |
| `middleware.Auth` | componente; `h.Auth.Handle` exige `Authorization: Bearer <token>` de uma sessão ativa e guarda as claims em `c.Get(middleware.ClaimsKey)` |
| `middleware.Owner` | depois do `Auth`, só deixa passar se o `:id` da rota for o do usuário logado (403 se não for) |

As senhas são guardadas com hash argon2id. Os tokens são HS256, e só esse algoritmo é aceito. O `exp` e as outras claims ficam por conta de quem cria o token.

**Sessão revogável.** No login, o `jti` do token é gravado em `users.token`, e o `Auth` só aceita o token cujo `jti` é o ativo. Um novo login invalida o token anterior, e o logout invalida o atual.

> `user` e `auth` são um exemplo, propositalmente simples: o cadastro aceita `profile` e não há rate limit no login.

## CLI

| Comando | O que faz |
|---|---|
| `prea run [-p 8000]` | compila e sobe o `./cmd/server`, repassando os sinais para o desligamento gracioso; a porta também pode vir de `$PORT` |
| `prea g p <Nome>` | cria `internal/preas/<nome>/` (module, handler, service, repository, model e dtos) e registra o módulo em `preas.go` |
| `prea g m <Nome> [--func]` | cria `internal/middleware/<nome>.go`: um componente (com injeção), ou uma função com `--func` |

O `g` é atalho de `generate`, o `p` de `prea` e o `m` de `middleware`. Rode a CLI na raiz do projeto: ela lê o `go.mod` para descobrir o caminho dos imports.

**Nomes:** letras, em camel case ou separadas por `_`. `OrderItem` e `order_item` são a mesma coisa; números não são aceitos (`order123`). Cada parte do código recebe a forma que a convenção pede:

| `prea g p order_item` | |
|---|---|
| pacote e pasta | `orderitem` (pacote Go não usa `_`) |
| tipos | `OrderItem` |
| tabela | `order_item` (que você cria no banco) |
| rotas | `/order-item` (GET, GET `/:id`, POST, PATCH `/:id`, DELETE `/:id`) |

No middleware, o arquivo segue a tabela: `prea g m rate_limit` cria `internal/middleware/rate_limit.go` com o tipo `RateLimit`.

**Antes de gerar, a CLI confere tudo; se algo falha, o projeto não é alterado:**
- o nome não pode ser uma palavra-chave do Go (`type`);
- o pacote não pode ter o nome de um import do `preas.go` (`core`, `time`);
- o preá ou o arquivo do middleware não pode existir;
- o middleware não pode ter o nome de algo já declarado no pacote (`Owner`);
- o `preas.go` precisa ter o bloco de imports entre parênteses e o `var Modules = []*core.Module{...}`.

## Configuração

As variáveis podem vir do ambiente ou de um `.env` na raiz (veja o `.env.example`):

| Variável | Uso |
|---|---|
| `PGCONN` | DSN do Postgres, ex.: `postgres://user:pass@localhost:5432/prea` |
| `JWTKEY` | segredo do JWT, com 32+ bytes; **obrigatório** se a app usa o pacote `security`, ou ela não sobe |
| `PORT` | porta HTTP (padrão `8000`) |
| `GIN_MODE` | `debug` (padrão), `release` ou `test`; use `release` em produção. Pode ficar no `.env` |
| `LOG_LEVEL` | `debug`, `info` (padrão), `success` ou `error`; um valor inválido impede a subida |
| `LOG_FORMAT` | `text` (padrão) ou `json` |

## Testes

```bash
go test ./...

# sem os testes que compilam um projeto gerado
go test -short ./...

# inclui os testes de integração do banco (criam e apagam as tabelas "widgets" e "order_item")
PREA_TEST_DATABASE_URL="postgres://user:pass@localhost:5432/prea_test?sslmode=disable" go test ./...
```

O gerador é testado em três camadas (`cmd/prea`):
- **unidade:** formas dos nomes, nomes inválidos e a edição do `preas.go` em vários formatos (lista vazia, numa linha, com comentários, em grupo `var (...)`, import com alias);
- **comportamento:** gera num projeto temporário e confere arquivos, registro e conteúdo; todo comando que falha deixa o projeto intacto;
- **projeto gerado:** gera preás e middlewares num projeto novo, que usa este framework via `replace`, e roda `go vet` e `go test -race` nele. O teste de dentro monta a app e confere as rotas; com o banco, faz o CRUD inteiro pelo HTTP.

## Licença

Veja [LICENSE](LICENSE).
