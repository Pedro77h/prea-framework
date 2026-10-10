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
	Service *Service         `inject:""`
	Auth    *middleware.Auth `inject:""`
}

var _ core.Router = (*Handler)(nil)

func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/users")
	g.POST("", h.create)
	g.GET("", h.Auth.Handle, h.list)
	g.GET("/:id", h.Auth.Handle, h.get)
	g.PATCH("/:id", h.Auth.Handle, middleware.Owner, h.update)
	g.DELETE("/:id", h.Auth.Handle, middleware.Owner, h.delete)
}

func (h *Handler) list(c *gin.Context) {
	users, err := h.Service.List(c.Request.Context())
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Reply(c, http.StatusOK, core.Data(users))
}

func (h *Handler) get(c *gin.Context) {
	u, err := h.Service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		core.Error(c, err)
		return
	}
	core.Reply(c, http.StatusOK, core.Data(u))
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
	core.Reply(c, http.StatusCreated, core.Data(u))
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
	core.Reply(c, http.StatusOK, core.Data(u))
}

func (h *Handler) delete(c *gin.Context) {
	if err := h.Service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		core.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
