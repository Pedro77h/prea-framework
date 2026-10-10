package auth

import (
	"prea/core"
	"prea/internal/preas/user"
)

var Module = &core.Module{
	Name:    "auth",
	Imports: []*core.Module{user.Module},
	Providers: []any{
		new(Service),
		new(Handler),
	},
}
