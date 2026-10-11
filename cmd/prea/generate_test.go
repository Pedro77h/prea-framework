package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

const fixtureRegistry = `package preas

import (
	"time"

	"prea/core"
)

var Modules = []*core.Module{}

var Server = core.Server{ShutdownTimeout: 15 * time.Second}

var Middlewares = []any{core.RequestLogger, core.Recovery}
`

const fixtureMiddleware = `package middleware

import "github.com/gin-gonic/gin"

type Auth struct{}

func (a *Auth) Handle(c *gin.Context) { c.Next() }

func Owner(c *gin.Context) { c.Next() }
`

// newProject creates a project in a temp dir and moves into it.
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	write(t, filepath.Join(dir, registryFile), fixtureRegistry)
	write(t, filepath.Join(dir, middlewareDir, "auth.go"), fixtureMiddleware)
	t.Chdir(dir)
	return dir
}

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshot reads every file under the current dir.
func snapshot(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(name)
		files[filepath.ToSlash(name)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func run(args ...string) error {
	cmd := &cli.Command{Name: "prea", Commands: []*cli.Command{generateCommand()}, Writer: io.Discard, ErrWriter: io.Discard}
	return cmd.Run(context.Background(), append([]string{"prea"}, args...))
}

func TestNames(t *testing.T) {
	for in, want := range map[string]templateData{
		"product":      {Pkg: "product", Name: "Product", Table: "product", Route: "product"},
		"Product":      {Pkg: "product", Name: "Product", Table: "product", Route: "product"},
		"order_item":   {Pkg: "orderitem", Name: "OrderItem", Table: "order_item", Route: "order-item"},
		"OrderItem":    {Pkg: "orderitem", Name: "OrderItem", Table: "order_item", Route: "order-item"},
		"orderItem":    {Pkg: "orderitem", Name: "OrderItem", Table: "order_item", Route: "order-item"},
		"Order_Item":   {Pkg: "orderitem", Name: "OrderItem", Table: "order_item", Route: "order-item"},
		"rate_limitBy": {Pkg: "ratelimitby", Name: "RateLimitBy", Table: "rate_limit_by", Route: "rate-limit-by"},
		"HTTPLog":      {Pkg: "httplog", Name: "HttpLog", Table: "http_log", Route: "http-log"},
		"a_b_c":        {Pkg: "abc", Name: "ABC", Table: "a_b_c", Route: "a-b-c"},
	} {
		want.Framework = frameworkModule
		if got := names(in); got != want {
			t.Errorf("names(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"product", "OrderItem", "order_item", "a_b_c"} {
		if !validName.MatchString(name) {
			t.Errorf("%q must be valid", name)
		}
	}
	for _, name := range []string{"", "order123", "1order", "order_", "_order", "order__item", "order-item", "order item", "pedido_ç", "../x", "order.item"} {
		if validName.MatchString(name) {
			t.Errorf("%q must be invalid", name)
		}
	}
}

func TestArgs(t *testing.T) {
	newProject(t)
	before := snapshot(t)
	for _, args := range [][]string{
		{"g", "p"},
		{"g", "p", "a", "b"},
		{"g", "p", "order123"},
		{"g", "m", "rate-limit"},
		{"generate", "prea", "type"}, // keyword
		{"g", "p", "core"},           // collides with an import of preas.go
		{"g", "p", "time"},
		{"g", "m", "owner"}, // Owner is declared in auth.go
		{"g", "m", "auth"},  // auth.go exists
	} {
		if err := run(args...); err == nil {
			t.Errorf("%v must fail", args)
		}
	}
	if after := snapshot(t); !maps.Equal(before, after) {
		t.Fatalf("a failed command must not touch the project:\n%v", slices.Sorted(maps.Keys(after)))
	}
}

func TestGeneratePrea(t *testing.T) {
	newProject(t)
	if err := run("g", "p", "order_item"); err != nil {
		t.Fatal(err)
	}

	files := snapshot(t)
	var got []string
	for name := range files {
		if strings.HasPrefix(name, "internal/preas/orderitem/") {
			got = append(got, strings.TrimPrefix(name, "internal/preas/orderitem/"))
		}
	}
	slices.Sort(got)
	want := []string{"dtos/create_request.go", "dtos/update_request.go", "handler.go", "model.go", "module.go", "repository.go", "service.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("files: %v", got)
	}

	for name, content := range files {
		if !strings.HasPrefix(name, "internal/preas/orderitem/") {
			continue
		}
		pkg := "orderitem"
		if strings.Contains(name, "/dtos/") {
			pkg = "dtos"
		}
		if !strings.HasPrefix(content, "package "+pkg+"\n") {
			t.Errorf("%s: wrong package", name)
		}
		if strings.Contains(content, "{{") || strings.Contains(content, "<no value>") {
			t.Errorf("%s: unrendered template", name)
		}
	}
	for name, sub := range map[string]string{
		"model.go":      `return "order_item"`,
		"handler.go":    `r.Group("/order-item")`,
		"repository.go": "database.BaseRepository[OrderItem]",
		"service.go":    `"example.com/app/internal/preas/orderitem/dtos"`,
		"module.go":     `Name: "orderitem"`,
	} {
		if !strings.Contains(files["internal/preas/orderitem/"+name], sub) {
			t.Errorf("%s must contain %s", name, sub)
		}
	}

	modules, imports := registry(t, files[filepath.ToSlash(registryFile)])
	if !slices.Equal(modules, []string{"orderitem.Module"}) || !slices.Contains(imports, "example.com/app/internal/preas/orderitem") {
		t.Fatalf("registry: %v %v", modules, imports)
	}

	// a second preá goes after the first one, in both lists
	if err := run("g", "p", "Product"); err != nil {
		t.Fatal(err)
	}
	modules, imports = registry(t, snapshot(t)[filepath.ToSlash(registryFile)])
	if !slices.Equal(modules, []string{"orderitem.Module", "product.Module"}) ||
		!slices.Equal(imports, []string{"time", "example.com/app/internal/preas/orderitem", "example.com/app/internal/preas/product", "prea/core"}) { // gofmt sorts the group
		t.Fatalf("registry after two: %v %v", modules, imports)
	}

	// the same preá again, by any spelling, fails and keeps everything
	before := snapshot(t)
	for _, name := range []string{"order_item", "OrderItem", "orderitem"} {
		if err := run("g", "p", name); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("%s twice: %v", name, err)
		}
	}
	if !maps.Equal(before, snapshot(t)) {
		t.Fatal("a repeated preá must not touch the project")
	}
}

// A broken registry is detected before any file is generated.
func TestGeneratePreaBadRegistry(t *testing.T) {
	for name, src := range map[string]string{
		"no Modules":         "package preas\n\nimport (\n\t\"prea/core\"\n)\n\nvar Other = []*core.Module{}\n",
		"single import":      "package preas\n\nimport \"prea/core\"\n\nvar Modules = []*core.Module{}\n",
		"syntax error":       "package preas\n\nvar Modules = []*core.Module{\n",
		"Modules not a list": "package preas\n\nimport (\n\t\"prea/core\"\n)\n\nvar Modules = core.All()\n",
	} {
		t.Run(name, func(t *testing.T) {
			newProject(t)
			write(t, registryFile, src)
			before := snapshot(t)
			if err := run("g", "p", "product"); err == nil {
				t.Fatal("must fail")
			}
			if !maps.Equal(before, snapshot(t)) {
				t.Fatal("must not touch the project")
			}
		})
	}
	t.Run("no registry", func(t *testing.T) {
		newProject(t)
		os.Remove(registryFile)
		if err := run("g", "p", "product"); err == nil {
			t.Fatal("must fail")
		}
		if _, err := os.Stat(filepath.Join(preasDir, "product")); err == nil {
			t.Fatal("must not create the preá")
		}
	})
	t.Run("no go.mod", func(t *testing.T) {
		newProject(t)
		os.Remove("go.mod")
		if err := run("g", "p", "product"); err == nil || !strings.Contains(err.Error(), "go.mod") {
			t.Fatalf("must ask for the project root: %v", err)
		}
	})
}

// registered must handle the shapes a person leaves preas.go in.
func TestRegistered(t *testing.T) {
	const head = "package preas\n\nimport (\n\t\"time\"\n\n\t\"prea/core\"\n\t\"example.com/app/internal/preas/user\"\n\tx \"example.com/app/internal/preas/auth\"\n)\n\nvar _ = time.Second\n\n"
	for name, tc := range map[string]struct{ modules, want string }{
		"empty":               {"var Modules = []*core.Module{}", "product.Module"},
		"inline, no comma":    {"var Modules = []*core.Module{user.Module, x.Module}", "user.Module x.Module product.Module"},
		"multiline, comma":    {"var Modules = []*core.Module{\n\tuser.Module,\n\tx.Module,\n}", "user.Module x.Module product.Module"},
		"trailing comment":    {"var Modules = []*core.Module{\n\tuser.Module, // users\n\tx.Module, // auth\n}", "user.Module x.Module product.Module"},
		"comment after brace": {"var Modules = []*core.Module{user.Module, x.Module} // all", "user.Module x.Module product.Module"},
		"var group":           {"var (\n\tA = 1\n\tModules = []*core.Module{user.Module, x.Module}\n)", "user.Module x.Module product.Module"},
	} {
		t.Run(name, func(t *testing.T) {
			newProject(t)
			src := head + tc.modules + "\n"
			write(t, registryFile, src)
			out, err := registered("product", "example.com/app/internal/preas/product")
			if err != nil {
				t.Fatal(err)
			}
			modules, imports := registry(t, string(out))
			if strings.Join(modules, " ") != tc.want {
				t.Fatalf("modules: %v\n%s", modules, out)
			}
			if i := slices.Index(imports, "example.com/app/internal/preas/product"); i != slices.Index(imports, "example.com/app/internal/preas/auth")+1 {
				t.Fatalf("the import goes after the last preá: %v", imports)
			}
			if b, _ := os.ReadFile(registryFile); string(b) != src {
				t.Fatal("registered must not write the file")
			}
			if strings.Contains(tc.modules, "// users") && !strings.Contains(string(out), "// users") {
				t.Fatal("comments must be kept")
			}
		})
	}
	t.Run("aliased import with the same name", func(t *testing.T) {
		newProject(t)
		write(t, registryFile, head+"var Modules = []*core.Module{user.Module, x.Module}\n")
		if _, err := registered("x", "example.com/app/internal/preas/x"); err == nil {
			t.Fatal("x is already the alias of auth")
		}
	})
}

// registry returns the Modules elements and the imports of a preas.go.
func registry(t *testing.T, src string) (modules, imports []string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	for _, is := range file.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		imports = append(imports, p)
	}
	lit := findModulesList(file)
	if lit == nil {
		t.Fatal("Modules not found")
	}
	for _, e := range lit.Elts {
		sel := e.(*ast.SelectorExpr)
		modules = append(modules, sel.X.(*ast.Ident).Name+"."+sel.Sel.Name)
	}
	return modules, imports
}

func TestGenerateMiddleware(t *testing.T) {
	newProject(t)
	if err := run("g", "m", "rate_limit"); err != nil {
		t.Fatal(err)
	}
	if err := run("g", "m", "RequestTiming", "--func"); err != nil {
		t.Fatal(err)
	}
	files := snapshot(t)
	component, fn := files["internal/middleware/rate_limit.go"], files["internal/middleware/request_timing.go"]
	if !strings.Contains(component, "type RateLimit struct") || !strings.Contains(component, "func (m *RateLimit) Handle(c *gin.Context)") {
		t.Fatalf("component:\n%s", component)
	}
	if !strings.Contains(fn, "func RequestTiming(c *gin.Context)") || strings.Contains(fn, "struct") {
		t.Fatalf("func:\n%s", fn)
	}

	before := snapshot(t)
	for _, args := range [][]string{{"g", "m", "rate_limit"}, {"g", "m", "RateLimit", "--func"}, {"g", "m", "request_timing"}} {
		if err := run(args...); err == nil {
			t.Errorf("%v must fail", args)
		}
	}
	if !maps.Equal(before, snapshot(t)) {
		t.Fatal("must not touch the project")
	}
}

// Every template must render to valid, formatted Go for any valid name.
func TestTemplatesRender(t *testing.T) {
	for _, name := range []string{"x", "Product", "order_item", "HTTPLog", "a_b_c"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data := names(name)
			data.Import = "example.com/app/internal/preas/" + data.Pkg
			if err := renderAll(dir, data); err != nil {
				t.Fatal(err)
			}
			for _, tmpl := range []string{"templates/middleware.tmpl", "templates/middleware_func.tmpl"} {
				if err := render(filepath.Join(dir, filepath.Base(tmpl)+".go"), tmpl, data); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
