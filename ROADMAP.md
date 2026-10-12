# Roadmap do Preá

Este documento tem duas partes: os **gaps** do que já foi implementado (levantados lendo o código em 2026-10-10) e os **próximos passos** planejados, com uma proposta de desenho para cada um.

Severidade dos gaps: **Crítica** = quebra o isolamento ou a correção do framework; **Alta** = risco de segurança ou perda de dados; **Média** = afeta produção ou uso real; **Baixa** = qualidade ou ergonomia.

---

## Parte 1 — Gaps

### Segurança

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Sem refresh token: quando o token expira, é preciso fazer login de novo. | `auth` |

**Resolvidos:**
- O segredo do JWT (`security.jwt.secret`) é obrigatório, com pelo menos 32 bytes. Sem ele, a app não sobe (`security.SetSignKey` falha no `main`) e `CreateToken`/`DecodeToken` devolvem erro. Os testes definem `security.SignKey`.
- `DecodeToken` aceita só HS256 (`jwt.WithValidMethods`).
- CORS, CSRF (via `net/http.CrossOriginProtection`), headers de segurança e proxies confiáveis são configurados em `core.Security` no `preas.go`. Por padrão, nenhum proxy é confiável.
- Existe o 403 (`core.NewError(http.StatusForbidden, ...)`). O exemplo tem o `middleware.Owner`: só o dono altera ou apaga o próprio usuário.
- Revogação: o `jti` do token fica em `users.token` e o `middleware.Auth` confere com o banco. Um novo login invalida o token anterior, e existe o `POST /auth/logout`.
- O modo do gin vem de `server.mode`, que o `main` aplica depois de carregar a config. Antes, o gin lia o `GIN_MODE` antes de o `.env` ser carregado.

**Decisões (não são gaps):**
- O cadastro com `profile`, a listagem aberta para quem está logado e o login sem rate limit fazem parte do exemplo, que é propositalmente simples.
- Quem decide se o token tem `exp` é quem codifica o token.
- As rotas geradas são públicas; para fechá-las, use um middleware.

### Servidor (`core`)

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Erros de validação devolvem a mensagem crua do validator (`Key: 'CreateRequest.Email' Error:Field validation...`). Ver o passo 8. | handlers |

**Resolvidos:**
- Graceful shutdown (`core/server.go`): no SIGINT/SIGTERM o servidor para de aceitar conexões, espera as requisições em andamento (`ShutdownTimeout`) e só então retorna, então o pool do banco fecha depois. Se o tempo acaba, as conexões são fechadas e os contextos das requisições são cancelados. Testado com `-race`. O `prea run` roda o binário direto, porque o `go run` não repassa SIGTERM.
- Os timeouts do `http.Server` são configuráveis em `preas.Server` e têm padrões seguros.
- O banco recebe um `Ping` na subida, com timeout de 5s.
- Respostas: o envelope `{text, code, data}` é montado com `core.Reply` e partes (`Text`, `Code`, `Data`); o `core.Response` serve para formatos próprios. O JSON só sai indentado em modo debug.
- Logs: o pacote `logger` (slog) tem os níveis debug, info, success e error, configurados por `logging.level` e `logging.format`. O `core.RequestLogger` escolhe o nível pelo status e loga o erro real dos 500, e o `core.Recovery` loga panics com stack. A saída do gin vira debug.
- A config de segurança (headers, origens) é copiada quando o middleware é criado, então alterá-la depois não gera data race.

**Decisões (não são gaps):** health check, request id e métricas dependem da aplicação, e o usuário cria como módulo ou middleware.

### Configuração

| Sev. | Gap | Onde |
|---|---|---|
| — | Nenhum gap aberto. | |

**Resolvido (passo 1):** as variáveis de ambiente eram lidas em lugares diferentes, e o `.env` era carregado por um import com efeito colateral (`common/env.go`, removido). Agora tudo passa pelo pacote `config`, carregado uma vez no `main`; o `logger` e o `security` recebem a config na subida (`logger.Configure`, `security.SetSignKey`).

### Injeção de dependência (`di`)

| Sev. | Gap | Onde |
|---|---|---|
| — | Nenhum gap aberto. | |

**Resolvidos:**
- **Middleware como componente:**
  - `Module.Middlewares` aceita funções e componentes (`middleware.Auth{}`); o componente é criado, injetado e passa por `Init`/`Close` como qualquer outro, e é a mesma instância se também estiver em `Providers`;
  - `preas.Middlewares` e `Security.Middlewares` também aceitam componentes: eles vivem num módulo implícito `app` que importa todos os módulos, então injetam os `Globals` e os exports de qualquer módulo;
  - erros (ponteiro, struct sem `Handle`, dependência faltando) aparecem na subida;
  - o `prea g m` gera componente por padrão, e função com `--func`.
- **A tag curta `inject` foi removida.** Ela não era uma tag Go válida (o `go vet` acusava) e agora dá erro na subida com a correção; só `inject:""` é aceita.
- **Ciclo de vida:**
  - hooks opcionais `Init(ctx) error` e `Close(ctx) error`/`io.Closer`;
  - o `Init` roda na ordem das dependências, inclusive entre módulos, e o `Close` na ordem inversa, no desligamento gracioso e antes do pool do banco fechar;
  - se um `Init` falha, a app não sobe e o que já tinha iniciado é fechado;
  - o `Init` cobre o caso de "provider por construtor": lógica de criação que pode falhar;
  - para testes, existe o `core.App.Build(ctx)`, que devolve o engine e o `closer`.
- **Componentes nomeados**, no estilo dos tokens do Angular:
  - `di.Named("analytics", x)` em providers e globals, e `inject:"analytics"` no campo;
  - `inject:""` prefere o componente sem nome;
  - **comportamento corrigido:** antes, com dois candidatos do mesmo tipo, o primeiro era escolhido em silêncio. Agora isso é erro na subida, e a mensagem pede um nome.

**Resolvido (Crítica):**
- **O problema:** os módulos são variáveis de pacote, e os providers eram instâncias criadas uma vez por processo (`new(Repository)`). Um segundo `App` reaproveitava as instâncias, e como campos já preenchidos são mantidos, continuava com as dependências do primeiro container, sem erro nenhum. Dois containers em paralelo escreviam nos mesmos campos, o que é uma data race.
- **A solução:** os providers passaram a ser declarados como valor (`Repository{}`) e funcionam como molde. A cada `Load`, o container cria a instância com `reflect.New` e copia o molde. O módulo vira só declaração e nunca é alterado. Declarar um ponteiro dá erro com a correção na mensagem.
- **Testes:**
  - `di.TestIsolation`: 8 containers em paralelo com `-race`, cada um com o seu banco, nenhum componente compartilhado e a declaração intacta;
  - `core.TestAppsAreIsolated`: dois `App` montados a partir do mesmo módulo. Esse teste falha no código antigo (`app with "test" answered "prod"`).

**Plano futuro:** memória por requisição, como o `SecurityContextHolder.getContext()` e o `@RequestScope` do Spring, que guardam o dado num `ThreadLocal` acessível de qualquer lugar. Por enquanto o framework só tem singletons por container, e o estado de requisição vai explicitamente no `context.Context`/`gin.Context`, como as claims do `Auth`. Em Go não existe `ThreadLocal`; o motivo está detalhado em [docs/injecao-de-dependencia.md](docs/injecao-de-dependencia.md#9-estado-por-requisição-não-há-securitycontextholder).

### Banco (`database`)

| Sev. | Gap | Onde |
|---|---|---|
| Média | Só Postgres, e os tipos do pgx aparecem na API pública (`Conn`, `BaseRepository.DB *pgxpool.Pool`). **Decisão:** resolvido pelo multi-banco (passo 4) com os profiles (passo 1). | `database` |
| Média | `GetAll` não tem paginação nem limite, então a resposta cresce sem teto. **Decisão:** resolvido pelo specification util com paginação (passo 9). | `database/repository.go` |
| Média | Migrations: só existe um `.sql` solto, sem runner e sem versionamento. **Decisão:** resolvido pelas migrations do usuário (passo 2). | `internal/preas/user/migrations` |
| Baixa | A chave primária é sempre `id`; tabela e rota usam o nome do pacote no singular. **Decisão:** a tag `pk` do passo 3 escolhe a chave. | `database`, templates |
| Baixa | O mapeamento é *lax*: um campo sem coluna correspondente fica zerado sem erro, então um typo passa despercebido. **Decisão:** as tags do passo 3 dizem qual campo corresponde a qual coluna, e o mapeamento pode ser validado contra elas. | `database/statement.go` |

**Resolvidos:**
- **Update que perdia zeros (Baixa → Alta):** um `Update` com struct ignorava campos zerados, então `Active = false` ou `Count = 0` sumiam sem erro, uma perda de dados silenciosa. Agora:
  - `Save(ctx, entidade)`, no estilo do `save` do JPA: grava todas as colunas, zeros inclusive; insere com PK zerada, senão atualiza (`ErrNotFound` se a linha não existe). O fluxo é carregar, alterar e salvar, dentro de uma `Transaction` quando precisa ser atômico;
  - o `Update` continua sendo *patch*, mas o DTO pode usar ponteiros: `nil` não mexe, e um ponteiro para zero grava o zero (testado). O template do `UpdateRequest` explica isso;
  - ficou para depois: lock otimista (o `@Version` do JPA), que depende de uma coluna de versão declarada pelas tags do passo 3.
- **`Count` e `Exists`:** com filtro `Args` (`nil` = tabela toda), usando o mesmo `where` do `Where`/`First`.
- **Seleção de campos:** os métodos prontos usam `select *`; para escolher as colunas, use o `Prepare`, que mapeia para o model ou para um DTO de projeção (`Prepare[Summary](r.DB, "select id, name ...")`).
- **Transação (`@Transactional`):** `database.Transactional` embutido no componente marca quais métodos são atômicos com `s.Transaction(ctx, func(ctx) error {...})`.
  - A transação viaja no `ctx`: todo `Statement` do mesmo pool que roda com esse `ctx` usa a transação, inclusive o `BaseRepository` e as queries de `r.Prepare`, sem mudar os repositórios.
  - Commit quando a função devolve `nil`; rollback em erro ou panic (o panic continua subindo).
  - Chamada dentro de outra transação, abre um savepoint: o erro desfaz só o trabalho interno e a externa decide se segue.
  - A chave da transação no `ctx` é o pool, então com mais de um banco uma transação não captura queries de outro.
  - Exemplo em `internal/preas/auth`: o `Login` grava a sessão (que revoga a anterior) e assina o token na mesma transação; se a assinatura falha, a sessão anterior continua valendo. Isso corrigia um bug real do exemplo.
  - Ficou para depois: nível de isolamento e transação só de leitura (`TransactionWith(ctx, opts, fn)`), quando houver necessidade.

### CLI

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Faltam `prea new` (scaffold de projeto), `prea migrate` e `prea dev` (hot reload); o `prea run` depende de `./cmd/server`. **Decisão:** vêm depois da separação em módulos (passo 5), junto do `prea.go` (passo 6). | `cmd/prea` |

**Resolvidos:**
- **Nomes:** letras em camel case ou separadas por `_` (`order_item`, `OrderItem`); números continuam recusados (`order123`). Cada parte recebe a forma da convenção: pacote `orderitem`, tipo `OrderItem`, tabela `order_item`, rota `/order-item` e arquivo de middleware `rate_limit.go`.
- **Validação antes de gerar:** palavra-chave do Go, pacote com o nome de um import do `preas.go`, preá ou middleware existente e identificador já declarado no pacote de middlewares. O `preas.go` novo é calculado e validado **antes** de criar qualquer arquivo; um comando que falha não altera o projeto.
- **Testes pesados do gerador** (`cmd/prea`), em três camadas:
  - unidade: nomes e a edição do `preas.go` em vários formatos;
  - comportamento: arquivos, conteúdo e registro, e o projeto intacto em todo erro;
  - projeto gerado: um projeto novo, ligado ao framework por `replace`, passa por `go vet` e `go test -race`; o teste de dentro confere as rotas e, com banco, faz o CRUD pelo HTTP. Mutações nos templates (tirar o `TrimSpace`, trocar a rota) são pegas.

**Decisão (não é gap):** o caminho fixo `frameworkModule = "prea"` está certo: com a separação em módulos, tudo será importado de `prea/<pacote>`.

### Estrutura e qualidade

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | O teste de integração do banco só roda com env; não há CI nem docker-compose para Postgres local. | — |

**Resolvidos:**
- **Código morto em `common`:** o pacote foi removido, junto com o `Stoi64` e o `ThrowException`, que não eram usados. Os erros genéricos (`ErrNotFound`, `ErrUnauthorized`...) foram substituídos: erros de HTTP usam `core.NewError(http.StatusX, msg)`, com as constantes do `net/http`, e cada pacote declara os próprios erros (`database.ErrNotFound`, `database.ErrConflict`, `security.ErrInvalidToken`), que o `core.Error` converte em status.

**Decisões (não são gaps):**
- A app de exemplo (`internal/preas`) sai do repositório quando o framework for dividido em módulos (passo 5).
- O acoplamento do `middleware.Auth` com `auth/dtos` é do exemplo. Os preás `user` e `auth` serão refeitos do zero, então eles não ganham testes de service e handler agora.

---

## Parte 2 — Próximos passos

### 1. ~~Configuração e profiles (estilo Spring)~~ (feito)

**Feito:**
- **Pacote `config`, sobre o koanf:** carrega `application.yaml` e completa com `application-<profile>.yaml`. O profile, sempre um só, vem de `PREA_PROFILE` ou de `prea run --profile`.
- **Ambiente:** só entra onde o yaml o referencia, com `${VAR}`. Não há sobrescrita automática por variável. O `.env` é lido sem substituir o que já está no ambiente.
- **Tag `value:"chave"` no `di`:** serve para valores soltos e para blocos inteiros (pelas tags `yaml`), com conversão de tipos (`"true"` → bool, `"30s"` → `time.Duration`). Uma chave que não está no yaml, ou um `${VAR}` não definido, deixa o campo zerado (`nil` em ponteiros). O `*config.Config` é injetável.
- **`core.Module.Profiles`:** restringe um módulo a um profile.
- **Os yaml são da aplicação, não do framework.** Na app de exemplo eles ficam em `internal/config/`; com o `prea new`, a pasta `config/` vem no boilerplate.

**Decisões:**
- YAML com o koanf, em vez de um loader próprio.
- Sem vários profiles ao mesmo tempo e sem middleware por profile.

**Ficou para depois:** validar blocos de config com tags, que vem com o passo 8.

### 2. Migrations do usuário

**Objetivo:** uma pasta onde o usuário escreve as próprias migrations e as roda pela CLI.

**Proposta:**
- `migrations/` na raiz do projeto, com a pasta configurável pelo profile.
- Arquivos `20261010120000_create_users.up.sql` e `.down.sql`.
- Comandos: `prea migrate create <nome>`, `prea migrate up`, `prea migrate down [n]` e `prea migrate status`.
- Uma tabela `prea_migrations` guarda versão e checksum, e o runner avisa se um arquivo já aplicado foi alterado.
- Cada migration roda numa transação, e um advisory lock evita que duas instâncias migrem ao mesmo tempo.
- Opcionalmente, rodar `up` na subida (`database.migrate_on_start: true`, desligado em prod por padrão).

**Decisões em aberto:** escrever um runner próprio ou embrulhar o **goose** (embutível, suporta migrations em Go e vários bancos). Recomendo avaliar o goose antes de escrever um.

### 3. Novas tags: schema e geração de migrations

**Objetivo:** descrever as colunas no model e gerar a migration a partir dele.

**Proposta:**
```go
type User struct {
	ID        string    `db:"id,pk,type=uuid,default=gen_random_uuid()"`
	Email     string    `db:"email,unique,notnull,size=255"`
	TeamID    string    `db:"team_id,fk=teams.id,index"`
	CreatedAt time.Time `db:"created_at,default=now()"`
}
```
- `prea g migration --from-models` compara os models com o schema atual (introspecção) e gera o `up`/`down` em `migrations/`, para revisar antes de aplicar.
- Resolve gaps do banco:
  - **PK fixa:** `pk` define a chave primária, e o `BaseRepository` passa a usá-la em vez de `id`;
  - **mapeamento lax:** com as colunas declaradas, o mapeamento pode ser estrito (um campo sem coluna vira erro na subida, não um zero silencioso);
  - **lock otimista:** uma tag `version` faz o `Save` checar e incrementar a versão, como o `@Version` do JPA.
- Outras tags possíveis: `value:"chave"` (config) e `profile:"dev"`. O `inject:"nome"` já existe.

**Decisões em aberto:** estender a tag `db` ou criar uma tag `column` separada; como representar rename de coluna, que um diff não consegue detectar.

### 4. Bancos SQL e NoSQL configuráveis

**Objetivo:** escolher o banco pela configuração, sem ficar preso ao Postgres.

**Proposta:**
- O `database` passa a definir só interfaces. Cada driver vira um módulo separado, para que o projeto baixe apenas o driver que usa: `database/postgres` (pgx), `database/mysql`, `database/sqlite` e `database/mongo`.
- No SQL, um `Dialect` cuida das diferenças: placeholders (`$1` ou `?`), quoting (`"` ou `` ` ``) e `RETURNING` (o MySQL não tem). Os parâmetros `@nome` do `Prepare` são traduzidos para o dialeto.
- No NoSQL, a parte comum é a interface do `BaseRepository` (`GetAll`, `GetByID`, `Where`, `First`, `Create`, `Update`, `Save`, `Count`, `Exists`, `Delete` com filtro `Args`, que vira `bson.M` no Mongo; o `Save` vira `replaceOne` e o `Count`, `countDocuments`). O `Prepare` (estilo PDO) continua só no SQL; o Mongo expõe a collection nativa para consultas próprias.
- Conexões nomeadas na config (`database.main`, `database.analytics`) e injetadas pelo nome (`di.Named` + `inject:"analytics"`, que já existem).
- Cada driver traduz os próprios erros para os erros do framework (`ErrConflict`, `ErrNotFound`...).
- Cada driver implementa o `database.Transactional` (transação no `ctx`), que já existe para o Postgres.

**Decisões em aberto:** até onde o Mongo entra no `BaseRepository`; usar `database/sql` como base dos drivers SQL que não são Postgres.

### 5. Separação em módulos Go

**Objetivo:** instalar cada parte com `go get`, em versões independentes.

**Proposta:**
- Um repositório com vários módulos (`go.work` para desenvolvimento local), cada um com tag própria (`core/v0.1.0`).

  | Módulo | Depende de |
  |---|---|
  | `prea/di` | nada |
  | `prea/config` | goccy/go-yaml, godotenv |
  | `prea/logger` | nada |
  | `prea/core` | `di`, `config`, `logger`, `database`, `security`, gin |
  | `prea/database` + um submódulo por driver | nada do framework |
  | `prea/security` | nada |
  | `prea/cli` (o atual `cmd/prea`) | nada do framework |

- A app de exemplo (`internal/preas`) sai do repositório do framework e vira o template do `prea new`.
- A CLI ganha `prea new` (o template de projeto), `prea migrate` (passo 2) e `prea dev` (hot reload).

**Decisões em aberto:**
- **O `core.Error` importa o `database` e o `security`** para converter os erros deles, então o módulo `core` traz junto o pgx, o jwt e o argon2. Se isso pesar, a saída é inverter: o `core.Error` reconhece qualquer erro com um método `Status() int`, e cada pacote implementa esse método nos próprios erros.
- **Caminho baixável.** O `go get` só baixa módulos cujo caminho começa com um domínio (`github.com/...`, `prea.dev/...`). Um caminho `prea/core` funciona com `replace`/`go.work`, mas não pelo proxy. Opções: um domínio de *vanity import* (`prea.dev/core`, com a `frameworkModule` da CLI apontando para ele), ou `github.com/<org>/prea/core`.
- Política de versão: todos os módulos com a mesma versão ou cada um com a sua.

### 6. `prea.go` como único ponto de contato com o framework

**Objetivo:** o projeto do usuário tem só código dele. O framework chega via `go mod` e não é editado, porque o cache de módulos do Go é somente leitura.

**Proposta:** a estrutura de um projeto:
```
meu-app/
├── go.mod          # require prea/core ...
├── prea.go         # único arquivo que conversa com o framework
├── config/         # application.yaml + profiles (vem com o prea new)
├── migrations/
├── middleware/
└── preas/
    └── user/ ...
```
```go
// prea.go
package main

func main() {
	prea.Run(prea.Config{
		Modules:     []*core.Module{user.Module, auth.Module},
		Middlewares: []any{core.RequestLogger, core.Recovery},
	})
}
```
- O `prea.Run` faz o que hoje está espalhado em `cmd/server/main.go` e `internal/preas/preas.go`: carrega a config e o profile, abre as conexões, roda as migrations (se configurado), monta o container e sobe o servidor com timeouts e graceful shutdown.
- O gerador passa a editar o `prea.go`.

**Depende de:** passos 1 e 5.

### 7. Lib de segurança mais completa

**Objetivo:** sair do "JWT + argon2 básico" para algo seguro por padrão.

**Proposta, por prioridade:**
1. ~~Segredo obrigatório e validado na subida; `jwt.WithValidMethods`.~~ Feito.
2. **Autorização**: `middleware.Require("admin")` com papéis e permissões vindos das claims (o 403 já existe, com `core.NewError`).
3. **Tokens**: issuer/audience e TTL configuráveis; refresh token com rotação. A revogação por `jti` já existe no exemplo (`users.token`); falta levá-la para o framework.
4. **Chaves**: suporte a RS256/EdDSA e rotação por `kid`.
5. **Senhas**: parâmetros do argon2 configuráveis; *rehash* no login quando os parâmetros mudam; política mínima de senha; hash fictício para usuário inexistente, deixando o tempo de resposta igual.
6. **Proteções HTTP**: CORS, CSRF e headers já existem em `core.Security`; falta um middleware de rate limit.
7. Um getter tipado para as claims (`security.Claims(c)`) no lugar de `c.Get("claims")`.

### 8. Validação de structs

**Objetivo:** validar DTOs e configs com um formato de erro padrão e legível, independente do gin.

**Hoje:** a validação é a do gin (`binding:"required,email"`, via go-playground/validator). Ela só roda no `ShouldBind`, e o erro sai cru, com o nome da struct Go.

**Proposta:**
- Um pacote `validate` com `validate.Struct(v) error`. O erro devolvido envolve `ErrInvalidInput` e traz a lista de campos:
  ```json
  {"text": "invalid input", "code": 2, "data": {"email": "must be a valid email", "name": "required"}}
  ```
- **Nomes dos campos** vêm da tag `json` (`email`, não `Email`), e há uma mensagem por regra, traduzível (pt/en).
- **Tag `validate:"..."`** própria do framework, separada do `binding` do gin, para funcionar fora do HTTP: services, configs (passo 1), jobs e mensagens de fila.
- **Integração:**
  - um `core.Bind(c, &req) error`, que faz bind e validação com o erro já no formato acima;
  - o `core.Error` reconhece o erro de validação e coloca os campos em `data`.
- **Regras próprias:** `validate.Register("cpf", fn)`. A validação de struct inteira, como `DataFim > DataInicio`, fica num método opcional `Validate() error` no DTO, que o framework chama depois das tags.
- **Configs:** validar a config na subida com as mesmas tags, por exemplo `validate:"required,min=32"` na chave do JWT.

**Decisões em aberto:** usar o go-playground/validator por baixo (que o gin já traz) ou motor próprio; e se `binding` continua aceito como alias.

**Depende de:** nada; a parte de configs combina com o passo 1.

### 9. Specification e paginação

**Objetivo:** filtros dinâmicos e paginados sem SQL na mão, no modelo do `SpecificationUtils` usado nos projetos Spring.

**Proposta:**
- Cada filtro "IfPresent" só restringe quando o valor existe (ponteiro não nil, slice não vazio, texto não em branco), então um DTO de filtro vira a consulta direto:
```go
spec := database.AllOf(
	database.EqualIfPresent("status", f.Status),
	database.GreaterOrEqualIfPresent("price", f.MinPrice),
	database.LessOrEqualIfPresent("price", f.MaxPrice),
	database.InIfPresent("city", f.Cities),
	database.LikeAnyIfPresent(f.Search, "title", "code", "address"),
)
page, err := r.Search(ctx, spec, database.PageRequest{Page: 0, Size: 20, Sort: "price desc"})
// page.Items, page.Total, page.Page, page.Size
```
- A spec gera SQL com parâmetros `@pN` e colunas com quoting, como o `Where` atual; `Sort` só aceita colunas do model, para não abrir injeção.
- `GetAll` ganha um limite padrão ou passa a ser paginado.
- `Count` e `Exists` (que já existem com `Args`) passam a aceitar uma spec; o `Search` usa o `Count` para preencher `page.Total`.

**Decisões em aberto:** caminhos com join (`agency.id` no Spring); como o Mongo traduz a spec (passo 4).

**Depende de:** nada; os dialetos do passo 4 passam a gerar o SQL da spec.

### Ordem sugerida

0. ~~Gaps de segurança, servidor e DI; no banco, transação, `Save`, `Count`/`Exists` e seleção de campos~~ (feito).
1. ~~**Config e profiles**: base para segredos, banco, migrations e `prea.go`~~ (feito).
2. **Migrations**: logo se torna necessário, e o runner também é usado pelo passo 3.
3. **Separação em módulos e `prea.go`**: estabiliza a fronteira entre framework e usuário antes de crescer a API.
4. **Multi-banco**: os drivers já nascem como módulos separados.
5. **Tags de schema e geração de migrations**: depende de migrations e dos dialetos.
6. **Segurança**: o restante do passo 7.
7. **Validação de structs** (passo 8): independente; pode entrar antes, se o formato de erro for prioridade.
8. **Specification e paginação** (passo 9): independente; resolve o `GetAll` sem limite.

Todos os gaps Média e de banco que restam já têm passo definido:
- só Postgres → passo 4;
- migrations → passo 2;
- PK fixa, mapeamento lax e lock otimista → passo 3;
- `GetAll` sem limite → passo 9.
