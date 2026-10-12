package core

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"prea/config"
	"prea/di"

	"github.com/gin-gonic/gin"
)

func trace(name string) Middleware {
	return func(c *gin.Context) {
		c.Header("X-Trace", strings.TrimPrefix(c.Writer.Header().Get("X-Trace")+">"+name, ">"))
		c.Next()
	}
}

type routes struct {
	path   string
	before []Middleware
}

func (r *routes) Register(router gin.IRouter) {
	router.GET(r.path, append(r.before, func(c *gin.Context) { c.String(200, "ok") })...)
}

type usersRoutes struct{ routes }
type adminRoutes struct{ routes }

func TestMiddlewareLevels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	users := &Module{
		Name:        "users",
		Providers:   []any{usersRoutes{routes{path: "/users"}}},
		Middlewares: []any{trace("users")},
	}
	admin := &Module{
		Name:        "admin",
		Imports:     []*Module{users},
		Providers:   []any{adminRoutes{routes{path: "/admin", before: []Middleware{trace("route")}}}},
		Middlewares: []any{trace("admin")},
	}

	// users is only reachable through admin's imports: it must still be mounted.
	engine, _, err := App{Modules: []*Module{admin}, Middlewares: []any{trace("global")}}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		"/users": "global>users",
		"/admin": "global>admin>route",
	} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("X-Trace") != want {
			t.Errorf("%s: %d trace %q, want %q", path, w.Code, w.Header().Get("X-Trace"), want)
		}
	}
}

func TestAbort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deny := func(c *gin.Context) { c.AbortWithStatus(401) }

	engine, _, err := App{Modules: []*Module{{
		Name:        "m",
		Providers:   []any{routes{path: "/x"}},
		Middlewares: []any{deny},
	}}}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != 401 || w.Body.Len() != 0 {
		t.Fatalf("aborted chain must not reach the handler: %d %q", w.Code, w.Body)
	}
}

type greeting struct{ text string }

type hello struct {
	G *greeting `inject:""`
}

func (h *hello) Register(r gin.IRouter) {
	r.GET("/hello", func(c *gin.Context) { c.String(200, h.G.text) })
}

var helloModule = &Module{Name: "hello", Providers: []any{hello{}}}

func TestAppsAreIsolated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, text := range []string{"prod", "test"} {
		engine, _, err := App{Modules: []*Module{helloModule}, Globals: []any{&greeting{text}}}.Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", "/hello", nil))
		if w.Body.String() != text {
			t.Fatalf("app with %q answered %q: components leaked between apps", text, w.Body)
		}
	}
}

type secret struct{ value string }

// guard is a middleware component: it needs injection.
type guard struct {
	Secret *secret `inject:""`
	inits  int
}

func (g *guard) Init(context.Context) error { g.inits++; return nil }

func (g *guard) Handle(c *gin.Context) {
	if c.GetHeader("X-Secret") != g.Secret.value {
		c.AbortWithStatus(401)
		return
	}
	c.Next()
}

// panel also injects the guard, to check both get the same instance.
type panel struct {
	Guard *guard `inject:""`
}

func (p *panel) Register(r gin.IRouter) {
	r.GET("/panel", func(c *gin.Context) { c.String(200, "%d", p.Guard.inits) })
}

func TestMiddlewareComponents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := func(engine *gin.Engine, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/panel", nil)
		r.Header.Set("X-Secret", header)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}

	for name, providers := range map[string][]any{
		"only in Middlewares":        {panel{}},
		"also declared in Providers": {guard{}, panel{}},
	} {
		admin := &Module{Name: "admin", Providers: providers, Middlewares: []any{trace("fn"), guard{}}}
		engine, _, err := App{Modules: []*Module{admin}, Globals: []any{&secret{"s3"}}}.Build(t.Context())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if w := request(engine, "wrong"); w.Code != 401 || w.Header().Get("X-Trace") != "fn" {
			t.Fatalf("%s: functions and components run in order: %d %v", name, w.Code, w.Header())
		}
		if w := request(engine, "s3"); w.Code != 200 || w.Body.String() != "1" {
			t.Fatalf("%s: one instance, injected and initialized once: %d %q", name, w.Code, w.Body)
		}
	}

	for want, mw := range map[string]any{
		"must be declared as a value":                                    &guard{},
		"must be a core.Middleware or a struct whose pointer has Handle": secret{},
		"no provider for *core.secret":                                   guard{},
	} {
		globals := []any{&secret{}}
		if want == "no provider for *core.secret" {
			globals = nil
		}
		_, _, err := App{Modules: []*Module{{Name: "m", Middlewares: []any{mw}}}, Globals: globals}.Build(t.Context())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

type events struct{ log []string }

// limiter is a global middleware component that injects a module's export.
type limiter struct {
	Secret *secret `inject:""`
	Events *events `inject:""`
}

func (l *limiter) Init(context.Context) error {
	l.Events.log = append(l.Events.log, "init")
	return nil
}
func (l *limiter) Close(context.Context) error {
	l.Events.log = append(l.Events.log, "close")
	return nil
}

func (l *limiter) Handle(c *gin.Context) { trace("limiter:" + l.Secret.value)(c) }

func TestGlobalMiddlewareComponents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ev := &events{}
	users := &Module{Name: "users", Providers: []any{di.Export(secret{"users"}), panel{}, guard{}}}
	engine, closer, err := App{
		Modules:     []*Module{users},
		Security:    Security{Middlewares: []any{limiter{}}},
		Middlewares: []any{trace("fn"), limiter{}},
		Globals:     []any{ev},
	}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/panel", nil))
	if got := w.Header().Get("X-Trace"); got != "limiter:users>fn>limiter:users" {
		t.Fatalf("security first, then the app's, in order, injected with the module export: %q", got)
	}
	if err := closer(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ev.log, ",") != "init,close" {
		t.Fatalf("one instance for both lists, managed by the container: %v", ev.log)
	}

	_, _, err = App{Middlewares: []any{limiter{}}, Globals: []any{ev}}.Build(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no provider for *core.secret") {
		t.Fatalf("a global component without its dependency must fail at startup: %v", err)
	}
}

type mailer struct {
	Addr string `value:"mail.addr"`
	Port int    `value:"mail.port"`
}

func (m *mailer) Register(r gin.IRouter) {
	r.GET("/mail", func(c *gin.Context) { c.String(200, "%s:%d", m.Addr, m.Port) })
}

func TestConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.New("prod", map[string]any{"mail": map[string]any{"addr": "smtp", "port": 25}})
	mail := &Module{Name: "mail", Providers: []any{mailer{}}}
	debug := &Module{Name: "debug", Providers: []any{routes{path: "/debug"}}, Profiles: []string{"dev"}}

	engine, _, err := App{Modules: []*Module{mail, debug}, Config: cfg}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"/mail": 200, "/debug": 404} {
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
		if path == "/mail" && w.Body.String() != "smtp:25" {
			t.Errorf("values not filled: %q", w.Body)
		}
	}

	if _, _, err := (App{Modules: []*Module{mail}}).Build(t.Context()); err == nil || !strings.Contains(err.Error(), "needs a config") {
		t.Fatalf("a value without config must fail at startup: %v", err)
	}
	empty := config.New("", nil)
	engine, _, err = App{Modules: []*Module{mail}, Config: empty}.Build(t.Context())
	if err != nil {
		t.Fatalf("a missing key must leave the field zero: %v", err)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/mail", nil))
	if w.Body.String() != ":0" {
		t.Fatalf("missing keys must stay zero: %q", w.Body)
	}
}
