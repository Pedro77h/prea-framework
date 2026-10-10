package core

import (
	"net/http/httptest"
	"strings"
	"testing"

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
		Providers:   []any{&usersRoutes{routes{path: "/users"}}},
		Middlewares: []Middleware{trace("users")},
	}
	admin := &Module{
		Name:        "admin",
		Imports:     []*Module{users},
		Providers:   []any{&adminRoutes{routes{path: "/admin", before: []Middleware{trace("route")}}}},
		Middlewares: []Middleware{trace("admin")},
	}

	// users is only reachable through admin's imports: it must still be mounted.
	engine, err := App{Modules: []*Module{admin}, Middlewares: []Middleware{trace("global")}}.Engine()
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

	engine, err := App{Modules: []*Module{{
		Name:        "m",
		Providers:   []any{&routes{path: "/x"}},
		Middlewares: []Middleware{deny},
	}}}.Engine()
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != 401 || w.Body.Len() != 0 {
		t.Fatalf("aborted chain must not reach the handler: %d %q", w.Code, w.Body)
	}
}
