package core

import (
	"errors"
	"net/http"
	"prea/common"

	"github.com/gin-gonic/gin"
)

// Codes of the standard envelope. Reply picks one from the HTTP status
// when Code is not given: < 400 Success, < 500 Warning, otherwise Danger.
const (
	SuccessCode = iota + 1
	WarningCode
	DangerCode
)

// Message is the standard envelope: {"text": "...", "code": 1, "data": ...}.
type Message struct {
	Text string `json:"text,omitempty"`
	Code int    `json:"code"`
	Data any    `json:"data,omitempty"`
}

// Part is one field of the envelope; combine them in Reply.
type Part func(*Message)

func Text(text string) Part { return func(m *Message) { m.Text = text } }
func Code(code int) Part    { return func(m *Message) { m.Code = code } }
func Data(data any) Part    { return func(m *Message) { m.Data = data } }

// Reply sends the standard envelope with any combination of parts:
//
//	core.Reply(c, 200, core.Text("saved"))
//	core.Reply(c, 200, core.Text("saved"), core.Code(core.WarningCode))
//	core.Reply(c, 201, core.Text("created"), core.Data(user))
func Reply(c *gin.Context, status int, parts ...Part) {
	var m Message
	for _, part := range parts {
		part(&m)
	}
	if m.Code == 0 {
		m.Code = codeFor(status)
	}
	Response(c, status, m)
}

// Response sends any body as JSON, for formats other than the envelope.
// It is indented in debug mode.
func Response(c *gin.Context, status int, body any) {
	if gin.IsDebugging() {
		c.IndentedJSON(status, body)
		return
	}
	c.JSON(status, body)
}

func codeFor(status int) int {
	switch {
	case status < 400:
		return SuccessCode
	case status < 500:
		return WarningCode
	default:
		return DangerCode
	}
}

// Error maps domain errors to HTTP statuses. Unknown errors become a 500
// with a generic message; the real error goes to gin's logger.
func Error(c *gin.Context, err error) {
	switch {
	case errors.Is(err, common.ErrNotFound):
		Reply(c, http.StatusNotFound, Text(err.Error()))
	case errors.Is(err, common.ErrInvalidInput):
		Reply(c, http.StatusBadRequest, Text(err.Error()))
	case errors.Is(err, common.ErrConflict):
		Reply(c, http.StatusConflict, Text("conflict"))
	case errors.Is(err, common.ErrUnauthorized):
		Reply(c, http.StatusUnauthorized, Text(err.Error()))
	case errors.Is(err, common.ErrForbidden):
		Reply(c, http.StatusForbidden, Text(err.Error()))
	default:
		c.Error(err)
		Reply(c, http.StatusInternalServerError, Text("internal server error"))
	}
}
