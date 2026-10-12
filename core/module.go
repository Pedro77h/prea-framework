package core

import (
	"fmt"
	"reflect"
	"slices"

	"prea/di"

	"github.com/gin-gonic/gin"
)

// Middleware runs before handlers: call c.Next() to continue or c.Abort() to stop.
// It can be used globally (App), per module (Module.Middlewares) or per route.
type Middleware = gin.HandlerFunc

// MiddlewareComponent is a middleware that needs injection: a struct with
// inject fields and a Handle method, managed by the container like any
// other component (see Module.Middlewares).
type MiddlewareComponent interface {
	Handle(c *gin.Context)
}

// Module declares a feature: its components (see di) and the middlewares
// applied to every route registered by its routers.
//
// Middlewares takes functions (core.Middleware) and components
// (middleware.Auth{}). A component is added to Providers when it is not
// declared there, so it is created, injected and goes through Init/Close;
// when it is, the same instance serves both.
//
// Profiles limits the module to those profiles (see App.Config); empty loads
// it always. It applies to the modules listed in App.Modules.
type Module struct {
	Name        string
	Imports     []*Module
	Providers   []any
	Middlewares []any
	Profiles    []string
}

func (m *Module) Declare() *di.Module {
	imports := make([]di.Declarer, len(m.Imports))
	for i, imp := range m.Imports {
		imports[i] = imp
	}

	providers := slices.Clone(m.Providers)
	for _, mw := range m.Middlewares {
		if _, isFunc := asFunc(mw); isFunc || declared(providers, mw) {
			continue
		}
		providers = append(providers, mw)
	}
	return &di.Module{Name: m.Name, Imports: imports, Providers: providers}
}

// middlewares resolves Middlewares against the components the container
// created for this module.
func (m *Module) middlewares(components []any) ([]Middleware, error) {
	handlers := make([]Middleware, 0, len(m.Middlewares))
	for _, mw := range m.Middlewares {
		if fn, ok := asFunc(mw); ok {
			handlers = append(handlers, fn)
			continue
		}

		want := reflect.PointerTo(reflect.TypeOf(mw))
		i := slices.IndexFunc(components, func(c any) bool { return reflect.TypeOf(c) == want })
		component, ok := MiddlewareComponent(nil), false
		if i >= 0 {
			component, ok = components[i].(MiddlewareComponent)
		}
		if !ok {
			return nil, fmt.Errorf("core: module %s: middleware %T must be a core.Middleware or a struct whose pointer has Handle(*gin.Context)", m.Name, mw)
		}
		handlers = append(handlers, component.Handle)
	}
	return handlers, nil
}

func asFunc(mw any) (Middleware, bool) {
	switch fn := mw.(type) {
	case Middleware:
		return fn, fn != nil
	case func(*gin.Context):
		return fn, fn != nil
	}
	return nil, false
}

func declared(providers []any, mw any) bool {
	return slices.ContainsFunc(providers, func(p any) bool {
		return reflect.TypeOf(di.Unwrap(p)) == reflect.TypeOf(mw)
	})
}
