package di

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
)

type component struct {
	value any
	name  string
}

type Container struct {
	globals   []component
	order     []Declarer
	providers map[Declarer][]component
	exports   map[Declarer][]component
	loading   map[Declarer]bool
	owned     map[any]bool
	deps      map[any][]any
	started   []any
}

func New(globals ...any) *Container {
	c := &Container{
		providers: map[Declarer][]component{},
		exports:   map[Declarer][]component{},
		loading:   map[Declarer]bool{},
		owned:     map[any]bool{},
		deps:      map[any][]any{},
	}
	for _, g := range globals {
		value, name, _ := unwrap(g)
		c.globals = append(c.globals, component{value, name})
	}
	return c
}

func (c *Container) Modules() []Declarer {
	return c.order
}

func (c *Container) Providers(m Declarer) []any {
	values := make([]any, len(c.providers[m]))
	for i, p := range c.providers[m] {
		values[i] = p.value
	}
	return values
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

	providers, exports, err := instantiate(spec.Providers)
	if err != nil {
		return fmt.Errorf("di: module %s: %w", spec.Name, err)
	}
	for _, p := range providers {
		c.owned[p.value] = true
	}

	scopes := [][]component{providers, imported, c.globals}
	for _, p := range providers {
		var deps []any
		if err := injectFields(reflect.ValueOf(p.value).Elem(), scopes, &deps); err != nil {
			return fmt.Errorf("di: module %s: %w", spec.Name, err)
		}
		for _, d := range deps {
			if c.owned[d] {
				c.deps[p.value] = append(c.deps[p.value], d)
			}
		}
	}

	c.order = append(c.order, m)
	c.providers[m] = providers
	c.exports[m] = exports
	return nil
}

func (c *Container) loadImports(spec *Module) ([]component, error) {
	var imported []component
	for _, imp := range spec.Imports {
		if err := c.load(imp); err != nil {
			return nil, err
		}
		imported = append(imported, c.exports[imp]...)
	}
	return imported, nil
}

func instantiate(declared []any) (providers, exports []component, err error) {
	type key struct {
		t    reflect.Type
		name string
	}
	seen := map[key]bool{}

	for _, d := range declared {
		value, name, isExported := unwrap(d)

		t := reflect.TypeOf(value)
		if t != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
			return nil, nil, fmt.Errorf("provider %v must be declared as a value, %v{}: each container creates its own instance", t, t.Elem())
		}
		if t == nil || t.Kind() != reflect.Struct {
			return nil, nil, fmt.Errorf("provider %v must be a struct value", t)
		}
		if seen[key{t, name}] {
			return nil, nil, fmt.Errorf("duplicate provider %v%s", t, describe(name))
		}
		seen[key{t, name}] = true

		instance := reflect.New(t)
		instance.Elem().Set(reflect.ValueOf(value))
		p := component{instance.Interface(), name}

		providers = append(providers, p)
		if isExported {
			exports = append(exports, p)
		}
	}
	return providers, exports, nil
}

func (c *Container) Init(ctx context.Context) error {
	visited := map[any]bool{}
	var visit func(p any) error
	visit = func(p any) error {
		if visited[p] {
			return nil
		}
		visited[p] = true
		for _, d := range c.deps[p] {
			if err := visit(d); err != nil {
				return err
			}
		}
		if i, ok := p.(Initializer); ok {
			if err := i.Init(ctx); err != nil {
				return fmt.Errorf("di: %T.Init: %w", p, err)
			}
		}
		c.started = append(c.started, p)
		return nil
	}

	for _, m := range c.order {
		for _, p := range c.providers[m] {
			if err := visit(p.value); err != nil {
				return errors.Join(err, c.Close(ctx))
			}
		}
	}
	return nil
}

func (c *Container) Close(ctx context.Context) error {
	var errs []error
	for i := len(c.started) - 1; i >= 0; i-- {
		var err error
		switch p := c.started[i].(type) {
		case Closer:
			err = p.Close(ctx)
		case io.Closer:
			err = p.Close()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("di: %T.Close: %w", c.started[i], err))
		}
	}
	c.started = nil
	return errors.Join(errs...)
}
