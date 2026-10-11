package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated code is the product: generate a preá and middlewares in a
// fresh project, then vet it and run a test inside it that mounts the app and,
// with PREA_TEST_DATABASE_URL, goes through the whole CRUD over HTTP.
func TestGeneratedProject(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a project")
	}
	framework, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(framework, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}

	dir := newProject(t)
	write(t, "go.mod", "module example.com/app\n\ngo 1.26.2\n\nrequire prea v0.0.0\n\nreplace prea => "+framework+"\n")
	write(t, "go.sum", string(sum))
	os.Remove(filepath.Join(middlewareDir, "auth.go"))

	for _, args := range [][]string{
		{"g", "p", "order_item"},
		{"g", "p", "Product"},
		{"g", "m", "rate_limit"},
		{"g", "m", "request_timing", "--func"},
	} {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	write(t, filepath.Join(preasDir, "app_test.go"), appTest)

	gocmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		// offline: every dependency is the framework's, already in the module cache
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	gocmd("mod", "tidy")
	gocmd("vet", "./...")
	gocmd("test", "-count=1", "-race", "./...")
}

const appTest = `package preas

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"example.com/app/internal/middleware"
	"prea/core"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	url := os.Getenv("PREA_TEST_DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), cmp.Or(url, "postgres://localhost:1/none"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	engine, closer, err := core.App{
		Modules:     Modules,
		Middlewares: append(slices.Clone(Middlewares), middleware.RateLimit{}, middleware.RequestTiming),
		Globals:     []any{pool},
	}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer closer(context.Background())

	var routes []string
	for _, r := range engine.Routes() {
		routes = append(routes, r.Method+" "+r.Path)
	}
	for _, base := range []string{"/order-item", "/product"} {
		for _, want := range []string{"GET " + base, "GET " + base + "/:id", "POST " + base, "PATCH " + base + "/:id", "DELETE " + base + "/:id"} {
			if !slices.Contains(routes, want) {
				t.Errorf("missing route %s in %v", want, routes)
			}
		}
	}
	if url == "" {
		t.Log("PREA_TEST_DATABASE_URL not set: CRUD skipped")
		return
	}

	if _, err := pool.Exec(t.Context(), ` + "`" + `drop table if exists order_item;
		create table order_item (id uuid primary key default gen_random_uuid(), name text not null)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	type reply struct {
		Code int
		Data struct{ ID, Name string }
	}
	call := func(method, path, body string, status int) reply {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		var out reply
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}

	created := call("POST", "/order-item", ` + "`" + `{"name": "  caneta  "}` + "`" + `, http.StatusCreated)
	if created.Data.ID == "" || created.Data.Name != "caneta" {
		t.Fatalf("create: %+v", created)
	}
	call("POST", "/order-item", ` + "`" + `{"name": ""}` + "`" + `, http.StatusBadRequest)
	call("POST", "/order-item", ` + "`" + `{"name": "   "}` + "`" + `, http.StatusBadRequest)
	if got := call("GET", "/order-item/"+created.Data.ID, "", http.StatusOK); got.Data.Name != "caneta" {
		t.Fatalf("get: %+v", got)
	}
	if got := call("PATCH", "/order-item/"+created.Data.ID, ` + "`" + `{"name": "lápis"}` + "`" + `, http.StatusOK); got.Data.Name != "lápis" {
		t.Fatalf("update: %+v", got)
	}
	call("GET", "/order-item", "", http.StatusOK)
	call("DELETE", "/order-item/"+created.Data.ID, "", http.StatusNoContent)
	call("GET", "/order-item/"+created.Data.ID, "", http.StatusNotFound)
	call("GET", "/order-item/not-a-uuid", "", http.StatusBadRequest)
}
`
