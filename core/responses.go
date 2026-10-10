package core

import (
	"errors"
	"net/http"
	"prea/common"

	"github.com/gin-gonic/gin"
)

const (
	SuccessCode = iota + 1
	WarningCode
	DangerCode
)

type Message struct {
	Text string `json:"text"`
	Code int    `json:"code"`
}

type DataMessage struct {
	Text string `json:"text"`
	Code int    `json:"code"`
	Data any    `json:"data"`
}

func Response(ctx *gin.Context, code int, data any) {
	ctx.IndentedJSON(code, data)
}

// Error maps domain errors to HTTP statuses. Unknown errors become a 500
// with a generic message; the real error goes to gin's logger.
func Error(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, common.ErrNotFound):
		Response(ctx, http.StatusNotFound, Message{Text: err.Error(), Code: WarningCode})
	case errors.Is(err, common.ErrInvalidInput):
		Response(ctx, http.StatusBadRequest, Message{Text: err.Error(), Code: WarningCode})
	case errors.Is(err, common.ErrConflict):
		Response(ctx, http.StatusConflict, Message{Text: "conflict", Code: WarningCode})
	case errors.Is(err, common.ErrUnauthorized):
		Response(ctx, http.StatusUnauthorized, Message{Text: err.Error(), Code: WarningCode})
	default:
		ctx.Error(err)
		Response(ctx, http.StatusInternalServerError, Message{Text: "internal server error", Code: DangerCode})
	}
}
