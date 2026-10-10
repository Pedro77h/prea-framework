package auth

import (
	"prea/core"
	"prea/internal/middleware"
	"prea/internal/preas/user"
)

var Module = &core.Module{
	Name:    "auth",
	Imports: []*core.Module{user.Module},
	Providers: []any{
		Service{},
		middleware.Auth{},
		Handler{},
	},
}
