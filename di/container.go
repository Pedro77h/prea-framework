package di

import (
	"fmt"
	"reflect"
)

type Container struct {
	globals   []any
	order     []Declarer
	providers map[Declarer][]any
	exports   map[Declarer][]any
	loading   map[Declarer]bool
}

func New(globals ...any) *Container {
	return &Container{
		globals:   globals,
		providers: map[Declarer][]any{},
		exports:   map[Declarer][]any{},
		loading:   map[Declarer]bool{},
	}
}

func (c *Container) Modules() []Declarer {
	return c.order
}

func (c *Container) Providers(m Declarer) []any {
	return c.providers[m]
}

func (c *Container) Load(modules ...Declarer) error {
	for _, m := range modules {
		if err := c.load(m); err != nil {
			return err
		}
	}
	return nil
}

func (c *Container) load(m Declarer) error {
	if _, loaded := c.providers[m]; loaded {
		return nil
	}
	spec := m.Declare()
	if c.loading[m] {
		return fmt.Errorf("di: import cycle at module %s", spec.Name)
	}
	c.loading[m] = true
	defer delete(c.loading, m)

	imported, err := c.loadImports(spec)
	if err != nil {
		return err
	}

	providers, exports, err := unpack(spec.Providers)
	if err != nil {
		return fmt.Errorf("di: module %s: %w", spec.Name, err)
	}

	scopes := [][]any{providers, imported, c.globals}
	for _, p := range providers {
		if err := injectFields(reflect.ValueOf(p).Elem(), scopes); err != nil {
			return fmt.Errorf("di: module %s: %w", spec.Name, err)
		}
	}

	c.order = append(c.order, m)
	c.providers[m] = providers
	c.exports[m] = exports
	return nil
}

func (c *Container) loadImports(spec *Module) ([]any, error) {
	var imported []any
	for _, imp := range spec.Imports {
		if err := c.load(imp); err != nil {
			return nil, err
		}
		imported = append(imported, c.exports[imp]...)
	}
	return imported, nil
}

func unpack(declared []any) (providers, exports []any, err error) {
	seen := map[reflect.Type]bool{}

	for _, p := range declared {
		e, isExported := p.(exported)
		if isExported {
			p = e.provider
		}

		t := reflect.TypeOf(p)
		if t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
			return nil, nil, fmt.Errorf("provider %v must be a pointer to struct", t)
		}
		if seen[t] {
			return nil, nil, fmt.Errorf("duplicate provider %v", t)
		}
		seen[t] = true

		providers = append(providers, p)
		if isExported {
			exports = append(exports, p)
		}
	}
	return providers, exports, nil
}
