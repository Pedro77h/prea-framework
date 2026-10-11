package main

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	"github.com/urfave/cli/v3"
)

const (
	frameworkModule = "prea"
	preasDir        = "internal/preas"
	middlewareDir   = "internal/middleware"
)

// templates/prea/<path>.tmpl becomes <preá dir>/<path>.go;
// templates/middleware(_func).tmpl becomes <middleware dir>/<name>.go.
//
//go:embed templates
var templatesFS embed.FS

// validName takes words of letters, in camel case or joined by "_":
// OrderItem, orderItem, order_item. Digits are not allowed.
var validName = regexp.MustCompile(`^[a-zA-Z]+(_[a-zA-Z]+)*$`)

type templateData struct {
	Pkg       string // orderitem: package and folder (Go packages have no "_")
	Name      string // OrderItem: types
	Table     string // order_item
	Route     string // order-item
	Framework string
	Import    string
}

// names derives every form of a valid name.
func names(name string) templateData {
	var words []string
	for part := range strings.SplitSeq(name, "_") {
		words = append(words, strings.Split(snakeCase(part), "_")...)
	}
	var camel strings.Builder
	for _, w := range words {
		camel.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return templateData{
		Pkg:       strings.Join(words, ""),
		Name:      camel.String(),
		Table:     strings.Join(words, "_"),
		Route:     strings.Join(words, "-"),
		Framework: frameworkModule,
	}
}

// snakeCase splits camel case like the database package does:
// OrderItem -> order_item, HTTPCode -> http_code.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func generateCommand() *cli.Command {
	return &cli.Command{
		Name:    "generate",
		Aliases: []string{"gen", "g"},
		Usage:   "Generate framework code",
		Commands: []*cli.Command{
			{
				Name:      "prea",
				Aliases:   []string{"p"},
				Usage:     "Create a new preá (module, handler, service, repository, model and dtos)",
				ArgsUsage: "<name>",
				Action:    withName(generatePrea),
			},
			{
				Name:      "middleware",
				Aliases:   []string{"m"},
				Usage:     "Create a middleware component in " + middlewareDir,
				ArgsUsage: "<name>",
				Flags: []cli.Flag{&cli.BoolFlag{
					Name:  "func",
					Usage: "plain function instead of a component (no injection; usable as a global middleware)",
				}},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return withName(func(name string) error {
						return generateMiddleware(name, cmd.Bool("func"))
					})(ctx, cmd)
				},
			},
		},
	}
}

func withName(generate func(name string) error) cli.ActionFunc {
	return func(_ context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 1 {
			return errors.New("expected exactly one argument: <name>")
		}
		name := cmd.Args().First()
		if !validName.MatchString(name) {
			return fmt.Errorf("invalid name %q: use letters, in camel case or separated by _ (OrderItem, order_item)", name)
		}
		return generate(name)
	}
}

func generatePrea(name string) error {
	project, err := projectModule()
	if err != nil {
		return err
	}

	data := names(name)
	pkg := data.Pkg
	if token.IsKeyword(pkg) {
		return fmt.Errorf("invalid name %q: %s is a Go keyword", name, pkg)
	}
	data.Import = path.Join(project, preasDir, pkg)
	dir := filepath.Join(preasDir, pkg)

	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("preá %q already exists", pkg)
	}
	registry, err := registered(pkg, data.Import)
	if err != nil {
		return fmt.Errorf("register module: %w", err)
	}

	if err := renderAll(dir, data); err != nil {
		os.RemoveAll(dir)
		return err
	}

	if err := os.WriteFile(registryFile, registry, 0o644); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("register module: %w", err)
	}

	fmt.Printf("preá %q created at %s and registered in %s\n", pkg, dir, registryFile)
	return nil
}

func generateMiddleware(name string, plainFunc bool) error {
	if _, err := projectModule(); err != nil {
		return err
	}

	data := names(name)
	data.Pkg = "middleware"
	dst := filepath.Join(middlewareDir, data.Table+".go")
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("middleware %s already exists", dst)
	}
	if file, ok, err := declaredIn(middlewareDir, data.Name); err != nil || ok {
		return cmp.Or(err, fmt.Errorf("%s is already declared in %s", data.Name, file))
	}

	tmpl := "templates/middleware.tmpl"
	if plainFunc {
		tmpl = "templates/middleware_func.tmpl"
	}
	if err := render(dst, tmpl, data); err != nil {
		return err
	}

	fmt.Printf("middleware %s created at %s\n", data.Name, dst)
	return nil
}

// declaredIn reports the file of dir that declares name at the top level.
func declaredIn(dir, name string) (string, bool, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
		if err != nil {
			return "", false, err
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == name {
					return f, true, nil
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						if sp.Name.Name == name {
							return f, true, nil
						}
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							if n.Name == name {
								return f, true, nil
							}
						}
					}
				}
			}
		}
	}
	return "", false, nil
}

// projectModule returns the module path declared in ./go.mod.
func projectModule() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", errors.New("go.mod not found: run prea from the project root")
	}
	for line := range strings.Lines(string(data)) {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(mod), `"`), nil
		}
	}
	return "", errors.New("go.mod: module directive not found")
}

func renderAll(dir string, data templateData) error {
	return fs.WalkDir(templatesFS, "templates/prea", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(name, "templates/prea/"), ".tmpl") + ".go"
		return render(filepath.Join(dir, filepath.FromSlash(rel)), name, data)
	})
}

func render(dst, name string, data templateData) error {
	tmpl, err := template.ParseFS(templatesFS, name)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", name, err)
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("format %s: %w", name, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, src, 0o644)
}
