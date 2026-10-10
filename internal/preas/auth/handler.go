package auth

import (
	"fmt"
	"net/http"
	"prea/common"
	"prea/core"
	"prea/internal/preas/auth/dtos"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Service *Service `inject:""`
}

var _ core.Router = (*Handler)(nil)

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/auth")
	g.POST("/login", h.login)
	g.POST("/validate", h.validate)
}

func (h *Handler) login(c *gin.Context) {
	var req dtos.LoginRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, fmt.Errorf("%w: %v", common.ErrInvalidInput, err))
		return
	}

	session, err := h.Service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusOK, session)
}

func (h *Handler) validate(c *gin.Context) {
	var req dtos.ValidateRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, fmt.Errorf("%w: %v", common.ErrInvalidInput, err))
		return
	}

	claims, err := h.Service.Validate(req.Token)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusOK, claims)
}
