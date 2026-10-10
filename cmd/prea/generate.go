package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/urfave/cli/v3"
)

const (
	frameworkModule = "prea"
	preasDir        = "internal/preas"
	middlewareDir   = "internal/middleware"
)

// templates/prea/<path>.tmpl becomes <preá dir>/<path>.go;
// templates/middleware.tmpl becomes <middleware dir>/<name>.go.
//
//go:embed templates
var templatesFS embed.FS

var validName = regexp.MustCompile(`^[a-zA-Z]+$`)

type templateData struct {
	Pkg, Name, Framework, Import string
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
				Usage:     "Create a middleware in " + middlewareDir,
				ArgsUsage: "<name>",
				Action:    withName(generateMiddleware),
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
			return fmt.Errorf("invalid name %q: only letters are allowed", name)
		}
		return generate(name)
	}
}

func generatePrea(name string) error {
	project, err := projectModule()
	if err != nil {
		return err
	}

	pkg := strings.ToLower(name)
	dir := filepath.Join(preasDir, pkg)

	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("preá %q already exists", pkg)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create preá folder: %w", err)
	}

	data := templateData{
		Pkg:       pkg,
		Name:      exported(name),
		Framework: frameworkModule,
		Import:    path.Join(project, preasDir, pkg),
	}

	if err := renderAll(dir, data); err != nil {
		os.RemoveAll(dir)
		return err
	}

	if err := registerModule(pkg, data.Import); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("register module: %w", err)
	}

	fmt.Printf("preá %q created at %s and registered in %s\n", pkg, dir, registryFile)
	return nil
}

func generateMiddleware(name string) error {
	if _, err := projectModule(); err != nil {
		return err
	}

	dst := filepath.Join(middlewareDir, strings.ToLower(name)+".go")
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("middleware %s already exists", dst)
	}

	data := templateData{Pkg: "middleware", Name: exported(name), Framework: frameworkModule}
	if err := render(dst, "templates/middleware.tmpl", data); err != nil {
		return err
	}

	fmt.Printf("middleware %s created at %s\n", data.Name, dst)
	return nil
}

func exported(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
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
