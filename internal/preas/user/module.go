package user

import (
	"prea/core"
	"prea/di"
	"prea/internal/middleware"
)

var Module = &core.Module{
	Name: "user",
	Providers: []any{
		Repository{},
		di.Export(Service{}),
		middleware.Auth{},
		Handler{},
	},
}
