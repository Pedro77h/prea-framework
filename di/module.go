package di

import "context"

type Module struct {
	Name      string
	Imports   []Declarer
	Providers []any
}

type Declarer interface {
	Declare() *Module
}

func (m *Module) Declare() *Module {
	return m
}

type Initializer interface {
	Init(ctx context.Context) error
}

// Values fills the fields tagged value:"key"; a missing key must leave dst
// as it is. It is resolved like an inject:"" field, so a global
// *config.Config is the usual source.
type Values interface {
	Value(key string, dst any) error
}

type Closer interface {
	Close(ctx context.Context) error
}

type exported struct {
	provider any
}

type named struct {
	name     string
	provider any
}

func Export(provider any) any {
	return exported{provider}
}

func Named(name string, provider any) any {
	return named{name, provider}
}

func unwrap(p any) (value any, name string, export bool) {
	for {
		switch w := p.(type) {
		case exported:
			export, p = true, w.provider
		case named:
			name, p = w.name, w.provider
		default:
			return p, name, export
		}
	}
}

func Unwrap(provider any) any {
	value, _, _ := unwrap(provider)
	return value
}
