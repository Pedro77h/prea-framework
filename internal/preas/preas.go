package preas

import (
	"time"

	"prea/core"
	"prea/internal/preas/auth"
	"prea/internal/preas/user"
)

var Modules = []*core.Module{
	user.Module,
	auth.Module,
}

var Security = core.Security{
	CORS:    &core.CORS{AllowOrigins: []string{"http://localhost:3000"}},
	CSRF:    &core.CSRF{},
	Headers: core.DefaultHeaders,
}

var Server = core.Server{
	ReadTimeout:     30 * time.Second,
	WriteTimeout:    30 * time.Second,
	ShutdownTimeout: 15 * time.Second,
}

var Middlewares = []core.Middleware{
	core.RequestLogger,
	core.Recovery,
}
