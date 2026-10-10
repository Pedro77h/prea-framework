package middleware

import (
	"fmt"
	"strings"

	"prea/common"
	"prea/core"
	"prea/internal/preas/auth/dtos"
	"prea/security"

	"github.com/gin-gonic/gin"
)

const ClaimsKey = "claims"

var _ core.Middleware = Auth

// Auth requires a valid "Authorization: Bearer <jwt>" header and stores the
// token claims in the context under ClaimsKey.
func Auth(c *gin.Context) {
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok {
		core.Error(c, fmt.Errorf("%w: missing bearer token", common.ErrUnauthorized))
		c.Abort()
		return
	}

	claims, err := security.Jwt[dtos.Payload]{}.DecodeToken(token)
	if err != nil {
		core.Error(c, fmt.Errorf("%w: invalid token", common.ErrUnauthorized))
		c.Abort()
		return
	}

	c.Set(ClaimsKey, claims)
	c.Next()
}
