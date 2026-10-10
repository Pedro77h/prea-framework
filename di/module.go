package di

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

type exported struct {
	provider any
}

func Export(provider any) any {
	return exported{provider}
}
