# Roadmap do Preá

Este documento tem duas partes: os **gaps** do que já foi implementado (levantados lendo o código em 2026-10-10) e os **próximos passos** planejados, com uma proposta de desenho para cada um.

Severidade dos gaps: **Alta** = risco de segurança ou perda de dados; **Média** = afeta produção ou uso real; **Baixa** = qualidade ou ergonomia.

---

## Parte 1 — Gaps

### Segurança

| Sev. | Gap | Onde |
|---|---|---|
| Alta | `JWTKEY` vazia é aceita: os tokens são assinados com chave vazia e qualquer um consegue forjar um token válido. | `security/jwt.go` (`init`) |
| Alta | O cadastro deixa o próprio usuário escolher o `profile` (`POST /users` com `"profile": 1`), ou seja, escalação de privilégio. | `internal/preas/user/dtos/create_request.go` |
| Alta | Só existe autenticação, não autorização: qualquer usuário logado lista, edita e apaga qualquer outro e vê os e-mails de todos. | `internal/preas/user/handler.go` |
| Alta | `DecodeToken` aceita token **sem `exp`** (não expira nunca) e não restringe o algoritmo (`jwt.WithValidMethods`). | `security/jwt.go` |
| Média | Login sem rate limit nem bloqueio, e responde mais rápido quando o usuário não existe (enumeração por tempo de resposta). | `internal/preas/auth/service.go` |
| Média | Sem refresh token e sem revogação; a coluna `users.token` existe mas ninguém usa. | `auth`, `user/model.go` |
| Média | As rotas geradas pelo template são públicas por padrão. | `cmd/prea/templates/prea/handler.tmpl` |
| Média | Sem CORS e sem headers de segurança; o gin confia em todos os proxies e roda em modo debug (os dois avisos aparecem na subida). | `core/app.go` |

### Servidor (`core`)

| Sev. | Gap | Onde |
|---|---|---|
| Média | Sem graceful shutdown: `engine.Run` derruba as requisições em andamento no SIGTERM, e o `defer db.Close()` nunca roda. | `core/app.go`, `cmd/server/main.go` |
| Média | Sem timeouts no `http.Server` (`ReadHeaderTimeout` etc.), o que deixa o servidor exposto a slowloris. | `core/app.go` |
| Média | O banco não é testado na subida (sem `Ping`): um DSN errado só aparece na primeira requisição. | `cmd/server/main.go` |
| Baixa | O formato das respostas é inconsistente: sucesso devolve o dado cru, erro devolve `{text, code}`. `DataMessage` e `SuccessCode` não são usados. As respostas usam `IndentedJSON` também em produção. | `core/responses.go` |
| Baixa | Erros de validação devolvem a mensagem crua do validator (`Key: 'CreateRequest.Email' Error:Field validation...`). | handlers |
| Baixa | Faltam health check, request id, logs estruturados (`slog`) e métricas; também falta `ErrForbidden` (403). | `core`, `common` |

### Configuração

| Sev. | Gap | Onde |
|---|---|---|
| Média | Variáveis de ambiente (`PGCONN`, `JWTKEY`, `PORT`) lidas em lugares diferentes e sem validação; o `.env` é carregado por um import com efeito colateral; não existem profiles. | `common/env.go`, `security/jwt.go`, `cmd/server/main.go` |

### Injeção de dependência (`di`)

| Sev. | Gap | Onde |
|---|---|---|
| Média | Os módulos são variáveis de pacote, então os componentes são singletons do processo. Um segundo `App` (em testes, por exemplo) reaproveita as instâncias, e como um campo já preenchido é mantido, **continua com as dependências do primeiro container**. | `di/inject.go`, `*/module.go` |
| Média | Não há providers por construtor nem por valor (config, clients), nem qualificadores (impossível ter dois `*pgxpool.Pool`), nem ciclo de vida (`Init`/`Close`), nem escopo por request. | `di` |
| Baixa | Middlewares são funções puras e não recebem injeção. | `core/module.go` |
| Baixa | A tag `inject` sem valor funciona, mas o `go vet` reclama dela; só `inject:""` passa limpo. | `di/inject.go` |

### Banco (`database`)

| Sev. | Gap | Onde |
|---|---|---|
| Média | Só Postgres, e os tipos do pgx aparecem na API pública (`Conn`, `BaseRepository.DB *pgxpool.Pool`). | `database` |
| Média | `GetAll` não tem paginação nem limite, então a resposta cresce sem teto. | `database/repository.go` |
| Média | Os métodos do `BaseRepository` não rodam dentro de transação (`DB` está preso ao pool) e não há helper de transação. | `database/repository.go` |
| Média | Migrations: só existe um `.sql` solto, sem runner e sem versionamento. | `internal/preas/user/migrations` |
| Baixa | A chave primária é sempre `id`; tabela e rota usam o nome do pacote no singular. | `database`, templates |
| Baixa | O mapeamento é *lax*: um campo sem coluna correspondente fica zerado sem erro, então um typo passa despercebido. | `database/statement.go` |
| Baixa | `Update` com struct ignora campos zerados; para gravar zero é preciso usar `Args`, o que é fácil de esquecer. | `database/repository.go` |
| Baixa | Não há `Count` nem `Exists`, e todas as queries usam `select *`. | `database/repository.go` |

### CLI

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Nomes aceitam só letras (`order_item` e números são recusados); o arquivo do middleware sai todo em minúsculo. | `cmd/prea/generate.go` |
| Baixa | O caminho do framework está fixo (`frameworkModule = "prea"`). | `cmd/prea/generate.go` |
| Baixa | Faltam `prea new` (scaffold de projeto), `prea migrate` e hot reload; o `prea run` depende de `./cmd/server`. | `cmd/prea` |
| Baixa | O gerador não tem testes, inclusive o `register.go`, que mexe na AST do `preas.go`. | `cmd/prea` |

### Estrutura e qualidade

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Código morto em `common` (`Stoi64`, `ThrowException`, `MakeLogger`, `ILogger`), e o nome `common` é genérico demais. | `common` |
| Baixa | A app de exemplo (`internal/preas`) mora dentro do repositório do framework. | `internal` |
| Baixa | `middleware.Auth` depende de `auth/dtos`, um acoplamento entre preás. | `internal/middleware/auth.go` |
| Baixa | Sem testes de services e handlers de `user`/`auth`; o teste de integração do banco só roda com env; não há CI nem docker-compose para Postgres local. | — |

---

## Parte 2 — Próximos passos

### 1. Configuração e profiles (estilo Spring)

**Objetivo:** ter um único lugar para a configuração, que muda por ambiente.

**Proposta:**
- Os arquivos ficam em `config/`: `application.yaml` é a base e `application-<profile>.yaml` sobrescreve por ambiente (`dev`, `test`, `prod`).
- O profile ativo vem de `PREA_PROFILE=dev` ou de `prea run --profile dev`.
- Variáveis de ambiente sobrescrevem qualquer chave (`PREA_DATABASE_URL` → `database.url`). Segredos ficam no ambiente, nunca no yaml commitado.
- A config tipada é injetável: `Config *config.App \`inject:""\``. Para valores soltos, uma nova tag: `URL string \`value:"database.url"\``.
- Módulos e middlewares podem ser restritos a um profile: `core.Module{Profiles: []string{"dev"}}`, por exemplo para endpoints de debug só em dev.
- A validação acontece na subida: falta de chave obrigatória ou `jwt.secret` curto demais impede o servidor de subir.

**Decisões em aberto:** yaml ou toml; usar uma lib pronta (koanf, viper) ou um loader próprio e mínimo.

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
- De quebra resolve a PK fixa: `pk` define a chave primária.
- Outras tags possíveis: `inject:"nome"` (qualificador), `value:"chave"` (config) e `profile:"dev"`.

**Decisões em aberto:** estender a tag `db` ou criar uma tag `column` separada; como representar rename de coluna, que um diff não consegue detectar.

### 4. Bancos SQL e NoSQL configuráveis

**Objetivo:** escolher o banco pela configuração, sem ficar preso ao Postgres.

**Proposta:**
- O `database` passa a definir só interfaces. Cada driver vira um módulo separado, para que o projeto baixe apenas o driver que usa: `database/postgres` (pgx), `database/mysql`, `database/sqlite` e `database/mongo`.
- No SQL, um `Dialect` cuida das diferenças: placeholders (`$1` ou `?`), quoting (`"` ou `` ` ``) e `RETURNING` (o MySQL não tem). Os parâmetros `@nome` do `Prepare` são traduzidos para o dialeto.
- No NoSQL, a parte comum é a interface do `BaseRepository` (`GetAll`, `GetByID`, `Where`, `First`, `Create`, `Update`, `Delete` com filtro `Args`, que vira `bson.M` no Mongo). O `Prepare` (estilo PDO) continua só no SQL; o Mongo expõe a collection nativa para consultas próprias.
- Conexões nomeadas na config (`database.main`, `database.analytics`) e injetadas com qualificador (`inject:"analytics"`).
- Cada driver traduz os próprios erros para os erros do framework (`ErrConflict`, `ErrNotFound`...).
- Os métodos do repositório passam a aceitar transação (`repo.WithTx(tx)` ou o contexto carregando a tx).

**Decisões em aberto:** até onde o Mongo entra no `BaseRepository`; usar `database/sql` como base dos drivers SQL que não são Postgres.

### 5. Separação em módulos Go

**Objetivo:** instalar cada parte com `go get`, em versões independentes.

**Proposta:**
- Um repositório com vários módulos (`go.work` para desenvolvimento local), cada um com tag própria (`core/v0.1.0`).

  | Módulo | Depende de |
  |---|---|
  | `github.com/<org>/prea/di` | nada |
  | `github.com/<org>/prea/errs` (o atual `common`, renomeado e sem código morto) | nada |
  | `github.com/<org>/prea/core` | `di`, `errs`, gin |
  | `github.com/<org>/prea/database` + um submódulo por driver | `errs` |
  | `github.com/<org>/prea/security` | `errs` |
  | `github.com/<org>/prea/cli` (o atual `cmd/prea`) | nada do framework |

- A app de exemplo (`internal/preas`) sai do repositório do framework e vira o template do `prea new`.
- A constante `frameworkModule` da CLI passa a apontar para o caminho real do módulo.

**Decisões em aberto:** org e caminho do módulo no GitHub; política de versão (todos os módulos com a mesma versão ou cada um com a sua).

### 6. `prea.go` como único ponto de contato com o framework

**Objetivo:** o projeto do usuário tem só código dele. O framework chega via `go mod` e não é editado, porque o cache de módulos do Go é somente leitura.

**Proposta:** a estrutura de um projeto:
```
meu-app/
├── go.mod          # require github.com/<org>/prea/core ...
├── prea.go         # único arquivo que conversa com o framework
├── config/         # application.yaml + profiles
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
		Middlewares: []core.Middleware{gin.Logger(), gin.Recovery()},
	})
}
```
- O `prea.Run` faz o que hoje está espalhado em `cmd/server/main.go` e `internal/preas/preas.go`: carrega a config e o profile, abre as conexões, roda as migrations (se configurado), monta o container e sobe o servidor com timeouts e graceful shutdown.
- O gerador passa a editar o `prea.go`.

**Depende de:** passos 1 e 5.

### 7. Lib de segurança mais completa

**Objetivo:** sair do "JWT + argon2 básico" para algo seguro por padrão.

**Proposta, por prioridade:**
1. **Corrigir os gaps Alta**: segredo obrigatório e longo, validado na subida; `exp` obrigatório; `jwt.WithValidMethods`; `profile` fora do cadastro.
2. **Autorização**: `middleware.Require("admin")` com papéis e permissões vindos das claims, e `ErrForbidden` (403).
3. **Tokens**: issuer/audience e TTL configuráveis; refresh token com rotação; revogação por `jti` (blacklist ou versão do token no usuário).
4. **Chaves**: suporte a RS256/EdDSA e rotação por `kid`.
5. **Senhas**: parâmetros do argon2 configuráveis; *rehash* no login quando os parâmetros mudam; política mínima de senha; hash fictício para usuário inexistente, deixando o tempo de resposta igual.
6. **Proteções HTTP**: rate limit e bloqueio no login, CORS configurável, headers de segurança, CSRF quando houver cookie.
7. Um getter tipado para as claims (`security.Claims(c)`) no lugar de `c.Get("claims")`.

### Ordem sugerida

0. **Gaps de severidade Alta** (passo 7.1). São pequenos e não dependem de nada.
1. **Config e profiles**: base para segredos, banco, migrations e `prea.go`.
2. **Migrations**: logo se torna necessário, e o runner também é usado pelo passo 3.
3. **Separação em módulos e `prea.go`**: estabiliza a fronteira entre framework e usuário antes de crescer a API.
4. **Multi-banco**: os drivers já nascem como módulos separados.
5. **Tags de schema e geração de migrations**: depende de migrations e dos dialetos.
6. **Segurança**: o restante do passo 7.

Os gaps de severidade Média de servidor e DI (graceful shutdown, timeouts, `Ping`, singletons) entram junto do `prea.Run` no passo 3.
