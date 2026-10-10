package core

import (
	"prea/di"

	"github.com/gin-gonic/gin"
)

// Router is implemented by components that expose HTTP routes.
type Router interface {
	Register(r gin.IRouter)
}

// App wires modules through the di container and serves their routes.
// Middlewares run on every request; Globals are injectable in every module.
type App struct {
	Modules     []*Module
	Middlewares []Middleware
	Globals     []any
}

func (a App) Engine() (*gin.Engine, error) {
	c := di.New(a.Globals...)
	for _, m := range a.Modules {
		if err := c.Load(m); err != nil {
			return nil, err
		}
	}

	engine := gin.New()
	engine.Use(a.Middlewares...)

	for _, m := range c.Modules() {
		group := engine.Group("")
		if mod, ok := m.(*Module); ok {
			group.Use(mod.Middlewares...)
		}
		for _, component := range c.Providers(m) {
			if router, ok := component.(Router); ok {
				router.Register(group)
			}
		}
	}
	return engine, nil
}

func (a App) Run(addr string) error {
	engine, err := a.Engine()
	if err != nil {
		return err
	}
	return engine.Run(addr)
}
