package preas

import (
	"prea/core"
	"prea/internal/preas/auth"
	"prea/internal/preas/user"

	"github.com/gin-gonic/gin"
)

var Modules = []*core.Module{
	user.Module,
	auth.Module,
}

var Middlewares = []core.Middleware{
	gin.Logger(),
	gin.Recovery(),
}
