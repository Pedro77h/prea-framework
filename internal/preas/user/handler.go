package user

import (
	"fmt"
	"net/http"
	"prea/common"
	"prea/core"
	"prea/internal/middleware"
	"prea/internal/preas/user/dtos"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Service *Service `inject:""`
}

var _ core.Router = (*Handler)(nil)

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/users")
	g.POST("", h.create)
	g.GET("", middleware.Auth, h.list)
	g.GET("/:id", middleware.Auth, h.get)
	g.PATCH("/:id", middleware.Auth, h.update)
	g.DELETE("/:id", middleware.Auth, h.delete)
}

func (h *Handler) list(c *gin.Context) {
	users, err := h.Service.List(c.Request.Context())
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusOK, users)
}

func (h *Handler) get(c *gin.Context) {
	u, err := h.Service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusOK, u)
}

func (h *Handler) create(c *gin.Context) {
	var req dtos.CreateRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, fmt.Errorf("%w: %v", common.ErrInvalidInput, err))
		return
	}

	u, err := h.Service.Create(c.Request.Context(), req)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusCreated, u)
}

func (h *Handler) update(c *gin.Context) {
	var req dtos.UpdateRequest
	if err := c.ShouldBind(&req); err != nil {
		core.Error(c, fmt.Errorf("%w: %v", common.ErrInvalidInput, err))
		return
	}

	u, err := h.Service.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Response(c, http.StatusOK, u)
}

func (h *Handler) delete(c *gin.Context) {
	if err := h.Service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		core.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
