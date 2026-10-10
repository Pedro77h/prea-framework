package core

import (
	"prea/di"

	"github.com/gin-gonic/gin"
)

// Middleware runs before handlers: call c.Next() to continue or c.Abort() to stop.
// It can be used globally (App), per module (Module.Middlewares) or per route.
type Middleware = gin.HandlerFunc

// Module declares a feature: its components (see di) and the middlewares
// applied to every route registered by its routers.
type Module struct {
	Name        string
	Imports     []*Module
	Providers   []any
	Middlewares []Middleware
}

func (m *Module) Declare() *di.Module {
	imports := make([]di.Declarer, len(m.Imports))
	for i, imp := range m.Imports {
		imports[i] = imp
	}
	return &di.Module{Name: m.Name, Imports: imports, Providers: m.Providers}
}
