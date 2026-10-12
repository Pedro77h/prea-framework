# Injeção de dependência no Preá

Este documento explica como o container do Preá (`prea/di`) cria, liga, inicia e encerra os componentes da aplicação, e compara cada conceito com o Spring e o Angular.

Resumo em uma frase: **um componente do Preá é o equivalente a um bean do Spring.** Ele é declarado num módulo, criado e injetado pelo container, singleton dentro do container, com hooks opcionais de subida (`Init` ≈ `@PostConstruct`) e de desligamento (`Close` ≈ `@PreDestroy`).

## Sumário

1. [Glossário: Preá × Spring × Angular](#1-glossário-preá--spring--angular)
2. [Declarando componentes](#2-declarando-componentes)
3. [Como o container monta a aplicação](#3-como-o-container-monta-a-aplicação)
4. [Regras de resolução](#4-regras-de-resolução)
5. [Componentes nomeados](#5-componentes-nomeados)
6. [Ciclo de vida: Init e Close](#6-ciclo-de-vida-init-e-close)
7. ["Construtores" em Go: por que o Init](#7-construtores-em-go-por-que-o-init)
8. [Isolamento: por que providers são valores](#8-isolamento-por-que-providers-são-valores)
9. [Estado por requisição: não há SecurityContextHolder](#9-estado-por-requisição-não-há-securitycontextholder)
10. [Testes](#10-testes)
11. [Erros e como resolver](#11-erros-e-como-resolver)
12. [Migração do formato antigo](#12-migração-do-formato-antigo)
13. [Limitações e planos](#13-limitações-e-planos)

---

## 1. Glossário: Preá × Spring × Angular

| Preá | Spring | Angular | Observação |
|---|---|---|---|
| componente (struct em `Providers`) | bean (`@Component`, `@Service`, `@Repository`) | provider (`@Injectable`) | |
| `core.Module` | classe `@Configuration` | `NgModule` | o Spring não tem visibilidade por módulo; o Preá segue o Angular |
| `Providers: []any{Service{}}` | `@ComponentScan` / `@Bean` | `providers: [Service]` | no Preá a lista é explícita, sem scan por reflexão |
| `di.Export(Service{})` | — (todo bean é visível) | `exports: [...]` | só o que é exportado fica visível para quem importa o módulo |
| `Imports: []*core.Module{user.Module}` | `@Import` | `imports: [...]` | |
| `core.App.Globals` | `registerSingleton(...)` / bean externo | `providedIn: 'root'` com valor (`useValue`) | instâncias prontas, criadas fora do container |
| campo com `inject:""` | `@Autowired` em campo | `inject(Service)` | |
| `di.Named("x", ...)` + `inject:"x"` | `@Qualifier("x")` | `InjectionToken` | |
| `Init(ctx) error` | `@PostConstruct` / `InitializingBean` | — (lógica no construtor ou em `useFactory`) | |
| `Close(ctx) error` / `io.Closer` | `@PreDestroy` / `DisposableBean` | `ngOnDestroy` | |
| singleton por container | escopo `singleton` (padrão) | singleton por injector | |
| — | `@RequestScope`, `SecurityContextHolder` | — | não existe no Preá (ver [seção 9](#9-estado-por-requisição-não-há-securitycontextholder)) |
| `di.Container` | `ApplicationContext` | `Injector` | |

## 2. Declarando componentes

Um módulo declara seus componentes **como valores**:

```go
// internal/preas/user/module.go
var Module = &core.Module{
	Name: "user",
	Providers: []any{
		Repository{},
		di.Export(Service{}), // visível para quem importar "user"
		middleware.Auth{},
		Handler{},
	},
}
```

Um componente é uma struct comum. As dependências são campos exportados com a tag `inject`:

```go
type Service struct {
	Repo *Repository `inject:""`
}
```

- O campo é sempre um **ponteiro** (`*Repository`) ou uma **interface**. O container entrega a instância que ele mesmo criou.
- O campo precisa ser **exportado** (letra maiúscula), porque o `reflect` não consegue escrever em campo privado.
- Use `inject:""`. A forma curta `inject` sem valor não é uma tag válida em Go e dá erro na subida: `S.F: use the tag inject:"" (a bare inject is not a valid Go struct tag)`.

Um módulo que depende de outro o importa e usa o que foi exportado:

```go
var Module = &core.Module{
	Name:      "auth",
	Imports:   []*core.Module{user.Module},
	Providers: []any{Service{}, middleware.Auth{}, Handler{}},
}

type Service struct {
	Users *user.Service `inject:""` // vem do export do módulo user
}
```

## 3. Como o container monta a aplicação

Quando `core.App` sobe (`Run`, `Serve` ou `Build`), acontece isto, nesta ordem:

```
1. Load      para cada módulo, primeiro os imports, depois ele mesmo:
             a. cria uma instância nova de cada provider (reflect.New + cópia da declaração)
             b. injeta os campos inject:"" de cada instância
             c. guarda quais componentes cada um recebeu (o grafo de dependências)
2. Init      chama Init(ctx) de cada componente, das dependências para os dependentes
3. Rotas     todo componente que implementa core.Router tem Register chamado
4. Serve     o servidor HTTP começa a aceitar requisições
   ...
5. Shutdown  SIGINT/SIGTERM: para de aceitar conexões e espera as requisições em andamento
6. Close     chama Close de cada componente, na ordem inversa do Init
7. (main)    o defer db.Close() fecha o pool do banco
```

Garantias que vêm dessa ordem:

- Nenhuma requisição chega antes de todos os `Init` terminarem.
- Nenhum `Close` roda enquanto ainda há requisição em andamento (dentro do `ShutdownTimeout`).
- Nenhum componente perde o banco antes do próprio `Close`.

Cada módulo é carregado **uma vez** por container, mesmo quando é importado por vários. Um ciclo de imports (A importa B, que importa A) é erro na subida.

## 4. Regras de resolução

Para cada campo `inject`, o container procura um componente nesta ordem de **escopos**, e para no primeiro escopo que tiver resposta:

1. os providers do **próprio módulo**;
2. os **exports dos módulos importados**;
3. os **globais** (`core.App.Globals`).

Dentro de um escopo, para um campo do tipo `T`:

1. **Tipo exato:** um componente cujo tipo é `T`.
2. **Interface:** se `T` é uma interface e não houve tipo exato, vale qualquer componente que implementa `T`.
3. **Nome:**
   - com `inject:"x"`, só entram os componentes nomeados `"x"`;
   - com `inject:""`, os sem nome têm preferência (ver [seção 5](#5-componentes-nomeados)).
4. **Exatamente um candidato** → injeta. **Nenhum** → passa para o próximo escopo. **Mais de um** → erro na subida, e o erro pede um nome. O container nunca escolhe em silêncio.

Outras regras:

- **Campos já preenchidos são mantidos.** Se a declaração já traz `Service{Repo: mock}`, o `Repo` não é sobrescrito.
- **Structs embutidas são percorridas.** É assim que o `database.BaseRepository[T]` embutido no seu repository recebe o `DB`.
- **Ciclos entre componentes são permitidos.** A pode injetar B e B injetar A, porque são ponteiros. Ciclos entre *módulos* não são permitidos.
- **Valores de config usam `value:"chave"`**, como o `@Value` do Spring: `Addr string \`value:"mail.addr"\``. A fonte é um componente que implementa `di.Values`, achado pelas mesmas regras de um `inject:""`; normalmente é o `*config.Config` que o `core.App.Config` coloca nos globais. Uma chave que não está no yaml deixa o campo zerado. Veja [Configuração no README](../README.md#configuração).

**Interfaces evitam ciclos de import entre pacotes.** O `middleware.Auth` precisa consultar a sessão do usuário, mas o pacote `user` já importa `middleware`. Por isso o `Auth` depende de uma interface, e o `user.Service` a implementa:

```go
// internal/middleware/auth.go
type Sessions interface {
	ActiveToken(ctx context.Context, userID string) (string, error)
}

type Auth struct {
	Sessions Sessions `inject:""` // o container encontra o user.Service
}
```

## 5. Componentes nomeados

O nome é opcional, como um `InjectionToken` do Angular ou um `@Qualifier` do Spring. Ele serve para quando há **mais de um componente do mesmo tipo**:

```go
// cmd/server/main.go
Globals: []any{mainDB, di.Named("analytics", analyticsDB)}
```

```go
type Reports struct {
	DB        *pgxpool.Pool `inject:""`          // mainDB (o sem nome)
	Analytics *pgxpool.Pool `inject:"analytics"` // analyticsDB
}
```

Em providers, `di.Named` e `di.Export` combinam em qualquer ordem:

```go
Providers: []any{
	di.Named("stripe", Gateway{}),
	di.Export(di.Named("paypal", Gateway{})), // o mesmo que di.Named("paypal", di.Export(Gateway{}))
}
```

| Situação | `inject:""` | `inject:"x"` |
|---|---|---|
| um componente sem nome | usa ele | erro: `no provider ... named "x"` |
| um sem nome + outros nomeados | usa o sem nome | usa o `"x"` |
| só um, nomeado `"x"` | usa ele | usa ele |
| dois sem nome do mesmo tipo | erro: `2 providers for ...` | — |

Dentro de um módulo, o mesmo tipo pode aparecer várias vezes, desde que **com nomes diferentes**. O mesmo tipo com o mesmo nome dá `duplicate provider`.

## 6. Ciclo de vida: Init e Close

Os dois hooks são **opcionais**. O container os chama se o componente implementar as interfaces:

```go
type Initializer interface{ Init(ctx context.Context) error } // di.Initializer
type Closer interface{ Close(ctx context.Context) error }     // di.Closer (io.Closer também vale)
```

Exemplo completo:

```go
type Mailer struct {
	Config *Config `inject:""`
	client *smtp.Client
}

func (m *Mailer) Init(ctx context.Context) error {
	if m.Config.SMTPAddr == "" {
		return errors.New("SMTP_ADDR is required")
	}
	c, err := smtp.Dial(m.Config.SMTPAddr)
	if err != nil {
		return err
	}
	m.client = c
	return nil
}

func (m *Mailer) Close(ctx context.Context) error {
	return m.client.Quit()
}
```

### Ordem

O `Init` segue o **grafo de dependências**, não a ordem de declaração. Se `API` injeta `Cache`, que injeta `Store`:

```
init store → init cache → init api      (subida)
close api → close cache → close store   (desligamento)
```

Isso vale **entre módulos**: se o `Store` está num módulo importado, ele inicia primeiro mesmo assim. Quando o seu `Init` roda, todas as dependências já passaram pelo `Init` delas.

### Falhas

- **`Init` com erro:** a app **não sobe**. Os componentes que já tinham iniciado recebem `Close`, em ordem inversa, e o erro diz quem falhou: `di: *mailer.Mailer.Init: SMTP_ADDR is required`.
- **`Close` com erro:** os outros `Close` rodam mesmo assim, e os erros são juntados (`errors.Join`) e devolvidos pelo `Run`.
- **O `ctx` do `Init`** é cancelado se chegar um SIGTERM durante a subida.
- **O `ctx` do `Close`** tem o prazo do `Server.ShutdownTimeout`.

### Quem passa pelo ciclo de vida

- **Passam:** os componentes criados pelo container, ou seja, os que estão em `Providers`.
- **Não passam:** os **globais** (`core.App.Globals`). Eles foram criados por outra pessoa, que é quem fecha. Por isso o pool do banco é fechado pelo `defer db.Close()` do `main`, **depois** de todos os `Close` dos componentes.
- O `Close` só roda em componentes que passaram pelo `Init`, e chamar o `Close` duas vezes não faz nada.

### Middlewares como componentes

Um middleware que precisa de injeção é um componente como qualquer outro: uma struct com campos `inject` e o método `Handle(c *gin.Context)` (interface `core.MiddlewareComponent`).

```go
type Auth struct {
	Sessions Sessions `inject:""`
}

func (a *Auth) Handle(c *gin.Context) { ... }

var Module = &core.Module{
	Name:        "admin",
	Middlewares: []any{middleware.Auth{}}, // todas as rotas do módulo
}
```

- **Gerenciado pelo container.** Ao montar o módulo, o `core.Module` adiciona o componente aos providers, se ele ainda não estiver lá. Assim, ele é criado por container, injetado e passa por `Init`/`Close`.
- **Mesma instância.** Se o mesmo tipo também está em `Providers`, porque o handler o injeta para usar por rota, é uma instância só.
- **Erros na subida.** Um ponteiro (`&middleware.Auth{}`), uma struct sem `Handle` e uma dependência faltando dão erro antes do servidor aceitar requisições.
- **Ordem.** `Module.Middlewares` aceita funções e componentes misturados, e eles rodam na ordem da lista.

**Globais.** `preas.Middlewares` e `Security.Middlewares` também aceitam componentes:

```go
var Middlewares = []any{core.RequestLogger, core.Recovery, middleware.RateLimit{}}
```

- **Módulo implícito `app`.** Um middleware global precisa de um escopo de onde injetar. O `App` monta um módulo `app` que **importa todos os módulos** da aplicação e declara os middlewares globais como providers. É a mesma regra de qualquer módulo: o componente enxerga os `Globals` e os **exports** (`di.Export`) dos módulos, nunca o que é privado deles.
- **Ciclo de vida.** Como está no container, recebe `Init` depois das dependências e `Close` antes delas.
- **Mesma instância.** Se o tipo aparece em `Security.Middlewares` e em `preas.Middlewares`, é uma instância só. Já um `middleware.Auth{}` global e outro em `Module.Middlewares` são instâncias diferentes, uma por módulo, como qualquer provider declarado em dois módulos.

## 7. "Construtores" em Go: por que o Init

Em Java, o Spring injeta pelo construtor (`public Service(Repository repo)`). Go **não tem construtores**: tem structs e, por convenção, funções `NewX(...)`. O Preá escolheu **injeção por campo + `Init`**:

| Necessidade | Spring | Preá |
|---|---|---|
| receber dependências | parâmetros do construtor / `@Autowired` | campos `inject:""` |
| lógica de criação (validar, abrir conexão) | corpo do construtor / `@PostConstruct` | `Init(ctx) error` |
| criação que pode falhar | exceção no construtor | `Init` devolve `error`, e a app não sobe |
| liberar recursos | `@PreDestroy` | `Close` |

O `Init` roda **depois** da injeção, então tem todas as dependências disponíveis, como o `@PostConstruct`. Na prática, ele faz o papel do construtor.

**Por que não `func NewService(repo *Repository) *Service`?** Seria possível, mas exigiria registrar uma função por componente e descobrir os parâmetros por reflexão. A declaração do módulo também ficaria mais pesada. Com campos, o componente se declara sozinho (`Service{}`), e o que ele precisa fica visível na struct. Providers por função construtora continuam como possibilidade futura, caso apareça um caso que o `Init` não cubra.

Regra prática:

- **Precisa de outro componente?** Campo com `inject:""`.
- **Precisa fazer algo depois de ter as dependências?** `Init`.
- **Segura um recurso** (conexão, arquivo, goroutine, buffer)? `Close`.

## 8. Isolamento: por que providers são valores

**Antes**, os providers eram ponteiros criados uma vez, quando o pacote era carregado:

```go
var Module = &core.Module{Providers: []any{new(Repository)}} // instância única no processo
```

Como os módulos são variáveis de pacote, **todo `core.App` do processo usava os mesmos objetos**. Um segundo `App`, num teste com outro banco por exemplo, encontrava o `Repository.DB` já preenchido pelo primeiro e o mantinha. O resultado era usar o banco errado sem nenhum erro. Dois containers em paralelo escreviam nos mesmos campos, o que é uma data race.

**Agora**, a declaração é um **molde**:

```go
var Module = &core.Module{Providers: []any{Repository{}}}
```

A cada `Load`, o container faz `reflect.New(Repository)`, copia o molde para a instância nova e injeta nela. Com isso:

- **cada container tem as próprias instâncias**, e dois `App` no mesmo processo não compartilham nada;
- **a declaração nunca é alterada**, então vários containers podem lê-la ao mesmo tempo sem data race;
- **valores preenchidos no molde são copiados** para a instância, como `routes{path: "/x"}` ou `Service{Repo: mock}`.

A cópia é **rasa**. Um ponteiro preenchido no molde (`Service{Repo: mock}`) aponta para o mesmo objeto em todos os containers que usarem essa declaração. Isso é intencional e fica a critério de quem declara.

Para garantir que só existe um jeito, declarar um ponteiro (`new(Repository)`) é **erro na subida**.

Isso é coberto por dois testes:
- `di.TestIsolation`: 8 containers em paralelo com `-race`;
- `core.TestAppsAreIsolated`: dois `App` montados a partir do mesmo módulo. Esse teste falha no código antigo.

## 9. Estado por requisição: não há SecurityContextHolder

No Spring, o `SecurityContextHolder.getContext()` devolve o usuário logado **de qualquer lugar**, porque guarda o dado num `ThreadLocal`, uma memória por thread, e cada requisição roda na sua thread. O `@RequestScope` funciona do mesmo jeito.

O Preá **não tem isso, por decisão**:

- **Em Go, uma requisição não tem uma thread fixa.** Uma requisição pode abrir várias goroutines, e goroutines não têm identidade nem memória local. Não existe `ThreadLocal`.
- **O jeito idiomático é passar o estado explicitamente** pelo `context.Context` (ou pelo `gin.Context` no handler). É o que todo o ecossistema Go faz: `database/sql`, `net/http`, gRPC, OpenTelemetry.

Como fica no Preá: o `middleware.Auth` guarda as claims no contexto da requisição, e quem precisa lê de lá:

```go
// handler
claims := c.MustGet(middleware.ClaimsKey).(security.DataClaims[dtos.Payload])
h.Service.DoSomething(c.Request.Context(), claims.Payload.ID)
```

Os componentes, por serem singletons compartilhados entre todas as requisições, **nunca guardam estado de requisição nos campos**. Esse estado vai nos parâmetros (`ctx`, ids) ou em variáveis locais. Estado compartilhado mutável, como um cache ou um contador, precisa de `sync.Mutex` ou `atomic`.

Escopo por request e um getter tipado como `security.Claims(ctx)` estão no ROADMAP como **plano futuro**.

## 10. Testes

**Testando um service com mock:** preencha o campo na declaração. Campos preenchidos não são sobrescritos. Para o fake caber no campo, o campo precisa ser uma **interface**:

```go
// no service: dependa do que você usa, não do tipo concreto
type Users interface {
	GetByID(ctx context.Context, id any) (User, error)
}

type Service struct {
	Repo Users `inject:""` // em produção, o container injeta o *Repository (único que implementa Users)
}

// no teste
type fakeUsers struct{}

func (fakeUsers) GetByID(context.Context, any) (user.User, error) { return user.User{Name: "fake"}, nil }

m := &core.Module{Name: "t", Providers: []any{user.Service{Repo: fakeUsers{}}}}
```

Com o campo concreto (`*Repository`, como no exemplo do repositório), o teste usa um banco de teste em `Globals`.

**Subindo a app inteira em teste:** use `Build`, que roda o `Init`, e feche no fim:

```go
engine, closer, err := core.App{Modules: preas.Modules, Globals: []any{testDB}}.Build(t.Context())
if err != nil {
	t.Fatal(err)
}
t.Cleanup(func() { closer(context.Background()) })

w := httptest.NewRecorder()
engine.ServeHTTP(w, httptest.NewRequest("GET", "/users", nil))
```

Como cada `Build` cria instâncias novas, vários testes, inclusive com `t.Parallel()`, podem montar a app com bancos diferentes sem interferir uns nos outros.

**Usando o container direto**, sem HTTP:

```go
c := di.New(testDB)
if err := c.Load(user.Module); err != nil { ... }
svc := c.Providers(user.Module)[1].(*user.Service) // na ordem da declaração
```

## 11. Erros e como resolver

Todos os erros aparecem **na subida**, nunca no meio de uma requisição. Eles vêm com o módulo e o caminho do campo, por exemplo `di: module auth: Service.Users: no provider for *user.Service`.

| Erro | Causa | Solução |
|---|---|---|
| `provider *X must be declared as a value, X{}: each container creates its own instance` | provider declarado como ponteiro (`new(X)`, `&X{}`) | declare `X{}` |
| `provider T must be a struct value` | provider que não é struct (string, `nil`...) | instâncias prontas vão em `Globals` |
| `duplicate provider X` / `duplicate provider X named "a"` | o mesmo tipo (e nome) duas vezes no módulo | remova um ou dê nomes diferentes com `di.Named` |
| `S.F must be exported to be injected` | campo `inject` com letra minúscula | exporte o campo |
| `S.F: no provider for *X` | nada fornece `*X` nos escopos do módulo | declare `X{}` no módulo, ou `di.Export` no módulo de origem e importe o módulo |
| `S.F: no provider for *X named "a"` | não há componente com esse nome | confira o `di.Named("a", ...)` |
| `S.F: 2 providers for *X: name them with di.Named and pick one with inject:"<name>"` | mais de um candidato | nomeie os componentes e use `inject:"nome"` |
| `S.F: use the tag inject:"" (a bare inject is not a valid Go struct tag)` | tag curta `inject` | troque por `inject:""` |
| `S.F: value:"k" needs a config (core.App.Config): no provider for di.Values` | campo `value` sem config na app | passe `Config: cfg` no `core.App` |
| `core: module m: middleware T must be a core.Middleware or a struct whose pointer has Handle(*gin.Context)` | item de `Module.Middlewares` que não é função nem componente | implemente `Handle(c *gin.Context)` com receiver ponteiro |
| `di: import cycle at module A` | A importa B, que importa A | extraia o que é comum para um terceiro módulo ou use uma interface |
| `di: *X.Init: ...` | o `Init` de X falhou | o resto da mensagem é o erro do seu `Init` |
| `di: *X.Close: ...` | o `Close` de X falhou no desligamento | os outros `Close` rodaram mesmo assim |

## 12. Migração do formato antigo

| Antes | Agora |
|---|---|
| `Providers: []any{new(Repository), di.Export(new(Service))}` | `Providers: []any{Repository{}, di.Export(Service{})}` |
| `engine, err := app.Engine()` | `engine, closer, err := app.Build(ctx)` (e chamar `closer` no fim) |
| dois globais do mesmo tipo: o primeiro era usado em silêncio | erro; use `di.Named` + `inject:"nome"` |
| dois exports importados do mesmo tipo: o primeiro era usado em silêncio | erro; idem |
| lógica de criação no `main` + `Globals` | `Init` no próprio componente |

O gerador (`prea g p`) já produz o formato novo.

## 13. Limitações e planos

- **Escopo por request / `SecurityContextHolder`:** não existe, pelo motivo da [seção 9](#9-estado-por-requisição-não-há-securitycontextholder). Está no ROADMAP como plano futuro.
- **Valor pronto por módulo:** uma instância criada fora do container só entra em `Globals`, que todos os módulos enxergam. Na maioria dos casos, o `Init` resolve: o próprio componente cria o recurso.
- **Providers por função construtora** (`func NewX(...) (*X, error)`): não existem. O `Init` cobre o mesmo caso; reavaliar se aparecer necessidade.
- **Profiles** (`profile:"dev"`, ativar componentes por ambiente): virão com os prea profiles (ROADMAP, passo 1).
