package user

import (
	"prea/core"
	"prea/di"
)

var Module = &core.Module{
	Name: "user",
	Providers: []any{
		new(Repository),
		di.Export(new(Service)),
		new(Handler),
	},
}
