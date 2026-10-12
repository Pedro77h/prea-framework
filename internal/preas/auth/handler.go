package auth

import (
	"net/http"
	"prea/core"
	"prea/internal/middleware"
	"prea/internal/preas/auth/dtos"
	"prea/security"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Service *Service         `inject:""`
	Auth    *middleware.Auth `inject:""`
}

var _ core.Router = (*Handler)(nil)

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/auth")
	g.POST("/login", h.login)
	g.POST("/validate", h.validate)
	g.POST("/logout", h.Auth.Handle, h.logout)
}

func (h *Handler) login(c *gin.Context) {
	var req dtos.LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, core.NewError(http.StatusBadRequest, err.Error()))
		return
	}

	session, err := h.Service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Reply(c, http.StatusOK, core.Data(session))
}

func (h *Handler) validate(c *gin.Context) {
	var req dtos.ValidateRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, core.NewError(http.StatusBadRequest, err.Error()))
		return
	}

	claims, err := h.Service.Validate(c.Request.Context(), req.Token)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Reply(c, http.StatusOK, core.Data(claims))
}

func (h *Handler) logout(c *gin.Context) {
	claims := c.MustGet(middleware.ClaimsKey).(security.DataClaims[dtos.Payload])
	if err := h.Service.Logout(c.Request.Context(), claims.Payload.ID); err != nil {
		core.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
