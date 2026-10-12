package middleware

import (
	"context"
	"net/http"
	"strings"

	"prea/core"
	"prea/internal/preas/auth/dtos"
	"prea/security"

	"github.com/gin-gonic/gin"
)

const ClaimsKey = "claims"

// Sessions returns the id of the user's active token (users.token).
// user.Service implements it; the interface avoids an import cycle.
type Sessions interface {
	ActiveToken(ctx context.Context, userID string) (string, error)
}

// Auth is a component: declare middleware.Auth{} in the module's Providers and inject it
// where routes are registered.
type Auth struct {
	Sessions Sessions `inject:""`
}

var _ core.Middleware = Owner

// Handle requires a valid "Authorization: Bearer <jwt>" header whose session
// is still active, and stores the claims in the context under ClaimsKey.
func (a *Auth) Handle(c *gin.Context) {
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok {
		core.Error(c, core.NewError(http.StatusUnauthorized, "missing bearer token"))
		c.Abort()
		return
	}

	claims, err := security.Jwt[dtos.Payload]{}.DecodeToken(token)
	if err != nil {
		core.Error(c, core.NewError(http.StatusUnauthorized, "invalid token"))
		c.Abort()
		return
	}

	active, err := a.Sessions.ActiveToken(c.Request.Context(), claims.Payload.ID)
	if err != nil || active != claims.ID {
		core.Error(c, core.NewError(http.StatusUnauthorized, "session ended"))
		c.Abort()
		return
	}

	c.Set(ClaimsKey, claims)
	c.Next()
}

// Owner runs after Auth and only lets the user whose id is in :id through.
func Owner(c *gin.Context) {
	claims := c.MustGet(ClaimsKey).(security.DataClaims[dtos.Payload])
	if claims.Payload.ID != c.Param("id") {
		core.Error(c, core.NewError(http.StatusForbidden, "not the owner"))
		c.Abort()
		return
	}
	c.Next()
}
