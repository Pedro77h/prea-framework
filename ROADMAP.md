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
- A `JWTKEY` é obrigatória, com pelo menos 32 bytes. Sem ela, a app não sobe (o `init` do `security` faz panic) e `CreateToken`/`DecodeToken` devolvem erro. Em `go test` não há panic; o teste define `security.SignKey`.
- `DecodeToken` aceita só HS256 (`jwt.WithValidMethods`).
- CORS, CSRF (via `net/http.CrossOriginProtection`), headers de segurança e proxies confiáveis são configurados em `core.Security` no `preas.go`. Por padrão, nenhum proxy é confiável.
- Foi criado o `ErrForbidden` (403). O exemplo tem o `middleware.Owner`: só o dono altera ou apaga o próprio usuário.
- Revogação: o `jti` do token fica em `users.token` e o `middleware.Auth` confere com o banco. Um novo login invalida o token anterior, e existe o `POST /auth/logout`.
- O `GIN_MODE` é lido do ambiente e do `.env`. Antes, o gin lia a variável antes de o `.env` ser carregado.

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
- Logs: o pacote `logger` (slog) tem os níveis debug, info, success e error, configurados por `LOG_LEVEL` e `LOG_FORMAT`. O `core.RequestLogger` escolhe o nível pelo status e loga o erro real dos 500, e o `core.Recovery` loga panics com stack. A saída do gin vira debug.
- A config de segurança (headers, origens) é copiada quando o middleware é criado, então alterá-la depois não gera data race.

**Decisões (não são gaps):** health check, request id e métricas dependem da aplicação, e o usuário cria como módulo ou middleware.

### Configuração

| Sev. | Gap | Onde |
|---|---|---|
| Média | As variáveis de ambiente (`PGCONN`, `JWTKEY`, `PORT`, `GIN_MODE`, `LOG_LEVEL`, `LOG_FORMAT`) são lidas em lugares diferentes, e o `.env` é carregado por um import com efeito colateral. | `common/env.go`, `security/jwt.go`, `logger`, `core/app.go`, `cmd/server/main.go` |

**Decisão:** este gap será resolvido pelos **prea profiles** (passo 1). Até lá, nada muda aqui, para não criar uma camada de config que seria substituída logo depois.

### Injeção de dependência (`di`)

| Sev. | Gap | Onde |
|---|---|---|
| Baixa | Não existe provider "valor pronto" por módulo: uma instância já criada fora do container, como um client configurado no `main`, só entra em `Globals`, que é visível para todos os módulos. Na maioria dos casos o `Init` resolve: o próprio componente cria o client. | `di` |
| Baixa | Um middleware só recebe injeção como componente (struct nos `Providers` e injetada no handler), então não dá para usá-lo em `Module.Middlewares`, que monta antes do container. | `core/module.go` |
| Baixa | A tag `inject` sem valor funciona, mas o `go vet` reclama dela; só `inject:""` passa limpo. | `di/inject.go` |

**Resolvidos:**
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

**Plano futuro:** escopo por request no container, um componente novo por requisição como no `@RequestScope` do Spring. Por enquanto o framework só tem singletons por container, e o estado de requisição vai no `context.Context`/`gin.Context`, como as claims do `Auth`.

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
| Baixa | Código morto em `common` (`Stoi64`, `ThrowException`), e o nome `common` é genérico demais. | `common` |
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
- Outras tags possíveis: `value:"chave"` (config) e `profile:"dev"`. O `inject:"nome"` já existe.

**Decisões em aberto:** estender a tag `db` ou criar uma tag `column` separada; como representar rename de coluna, que um diff não consegue detectar.

### 4. Bancos SQL e NoSQL configuráveis

**Objetivo:** escolher o banco pela configuração, sem ficar preso ao Postgres.

**Proposta:**
- O `database` passa a definir só interfaces. Cada driver vira um módulo separado, para que o projeto baixe apenas o driver que usa: `database/postgres` (pgx), `database/mysql`, `database/sqlite` e `database/mongo`.
- No SQL, um `Dialect` cuida das diferenças: placeholders (`$1` ou `?`), quoting (`"` ou `` ` ``) e `RETURNING` (o MySQL não tem). Os parâmetros `@nome` do `Prepare` são traduzidos para o dialeto.
- No NoSQL, a parte comum é a interface do `BaseRepository` (`GetAll`, `GetByID`, `Where`, `First`, `Create`, `Update`, `Delete` com filtro `Args`, que vira `bson.M` no Mongo). O `Prepare` (estilo PDO) continua só no SQL; o Mongo expõe a collection nativa para consultas próprias.
- Conexões nomeadas na config (`database.main`, `database.analytics`) e injetadas pelo nome (`di.Named` + `inject:"analytics"`, que já existem).
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
		Middlewares: []core.Middleware{core.RequestLogger, core.Recovery},
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
2. **Autorização**: `middleware.Require("admin")` com papéis e permissões vindos das claims (o `ErrForbidden` já existe).
3. **Tokens**: issuer/audience e TTL configuráveis; refresh token com rotação; revogação por `jti` (blacklist ou versão do token no usuário).
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

### Ordem sugerida

0. ~~Gaps de segurança~~ (feito).
1. **Config e profiles**: base para segredos, banco, migrations e `prea.go`.
2. **Migrations**: logo se torna necessário, e o runner também é usado pelo passo 3.
3. **Separação em módulos e `prea.go`**: estabiliza a fronteira entre framework e usuário antes de crescer a API.
4. **Multi-banco**: os drivers já nascem como módulos separados.
5. **Tags de schema e geração de migrations**: depende de migrations e dos dialetos.
6. **Segurança**: o restante do passo 7.
7. **Validação de structs** (passo 8): independente; pode entrar antes, se o formato de erro for prioridade.

Os gaps de severidade Média de servidor e DI (graceful shutdown, timeouts, `Ping`, singletons) entram junto do `prea.Run` no passo 3.
