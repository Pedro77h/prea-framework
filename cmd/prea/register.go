package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

var registryFile = filepath.Join(preasDir, "preas.go")

// registered returns registryFile with pkg.Module added to `var Modules` and
// its import, without writing it, so every check runs before any file is
// generated.
func registered(pkg, importPath string) ([]byte, error) {
	src, err := os.ReadFile(registryFile)
	if err != nil {
		return nil, fmt.Errorf("%w: run prea from the project root", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, registryFile, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", registryFile, err)
	}

	modules := findModulesList(file)
	if modules == nil {
		return nil, fmt.Errorf("%s: `var Modules = []*core.Module{...}` not found", registryFile)
	}
	for _, is := range file.Imports {
		if importName(is) == pkg {
			return nil, fmt.Errorf("%s already imports a package named %s (%s): pick another name", registryFile, pkg, is.Path.Value)
		}
	}
	lastImport := findLastImport(file, path.Dir(importPath)+"/")
	if lastImport == nil {
		return nil, fmt.Errorf("%s: parenthesized import block not found", registryFile)
	}

	offset := func(p token.Pos) int { return fset.Position(p).Offset }

	// Insert from the end of the file backwards so earlier offsets stay valid.
	entry := pkg + ".Module,\n"
	if n := len(modules.Elts); n > 0 && !bytes.Contains(src[offset(modules.Elts[n-1].End()):offset(modules.Rbrace)], []byte(",")) {
		entry = ", " + entry
	}
	src = insertAt(src, offset(modules.Rbrace), entry)
	src = insertAt(src, offset(lastImport.End()), "\n"+strconv.Quote(importPath))

	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("format %s: %w", registryFile, err)
	}
	return out, nil
}

func importName(is *ast.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	p, _ := strconv.Unquote(is.Path.Value)
	return path.Base(p)
}

func insertAt(src []byte, at int, text string) []byte {
	return append(src[:at:at], append([]byte(text), src[at:]...)...)
}

func findModulesList(file *ast.File) *ast.CompositeLit {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Names) == 1 && vs.Names[0].Name == "Modules" && len(vs.Values) == 1 {
				lit, _ := vs.Values[0].(*ast.CompositeLit)
				return lit
			}
		}
	}
	return nil
}

// findLastImport returns the last import starting with prefix, or the last import of the block.
func findLastImport(file *ast.File, prefix string) *ast.ImportSpec {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT || !gen.Lparen.IsValid() || len(gen.Specs) == 0 {
			continue
		}
		last := gen.Specs[len(gen.Specs)-1].(*ast.ImportSpec)
		for _, spec := range gen.Specs {
			if is := spec.(*ast.ImportSpec); strings.HasPrefix(is.Path.Value, `"`+prefix) {
				last = is
			}
		}
		return last
	}
	return nil
}
