package core

import (
	"context"
	"errors"
	"os"

	"prea/di"

	"github.com/gin-gonic/gin"
)

// Router is implemented by components that expose HTTP routes.
type Router interface {
	Register(r gin.IRouter)
}

// App wires modules through the di container and serves their routes.
// Security runs first, then Middlewares, on every request; Globals are
// injectable in every module.
type App struct {
	Modules     []*Module
	Security    Security
	Server      Server
	Middlewares []Middleware
	Globals     []any
}

// Build loads the modules, runs the components' Init (dependencies first)
// and mounts their routes. closer runs their Close in reverse order; call it
// once the engine stops serving. Globals are not closed: their owner does it.
func (a App) Build(ctx context.Context) (engine *gin.Engine, closer func(context.Context) error, err error) {
	c := di.New(a.Globals...)
	for _, m := range a.Modules {
		if err := c.Load(m); err != nil {
			return nil, nil, err
		}
	}
	if err := c.Init(ctx); err != nil {
		return nil, nil, err
	}
	engine, err = a.engine(c)
	if err != nil {
		return nil, nil, errors.Join(err, c.Close(ctx))
	}
	return engine, c.Close, nil
}

func (a App) engine(c *di.Container) (*gin.Engine, error) {
	// gin reads GIN_MODE before the .env is loaded, so read it again here.
	if mode := os.Getenv(gin.EnvGinMode); mode != "" {
		gin.SetMode(mode)
	}
	engine := gin.New()
	if err := a.Security.apply(engine); err != nil {
		return nil, err
	}
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
