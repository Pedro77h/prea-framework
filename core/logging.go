package core

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"prea/logger"

	"github.com/gin-gonic/gin"
)

func init() {
	// gin's debug output (routes, warnings) goes to logger.Debug.
	gin.DebugPrintFunc = func(format string, values ...any) {
		logger.Debug(strings.TrimSpace(fmt.Sprintf(format, values...)), "source", "gin")
	}
}

// RequestLogger logs one line per request: success below 400, info for 4xx
// and error for 5xx, with the errors passed to core.Error.
func RequestLogger(c *gin.Context) {
	start := time.Now()
	c.Next()

	status := c.Writer.Status()
	args := []any{
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"status", status,
		"latency", time.Since(start),
		"ip", c.ClientIP(),
	}
	if len(c.Errors) > 0 {
		args = append(args, "error", c.Errors.String())
	}

	switch {
	case status >= 500:
		logger.Error("request", args...)
	case status >= 400:
		logger.Info("request", args...)
	default:
		logger.Success("request", args...)
	}
}

// Recovery turns a panic into a 500 and logs it with the stack trace.
func Recovery(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok && strings.Contains(err.Error(), "broken pipe") {
				c.Abort() // client went away; nothing to answer
				return
			}
			logger.Error("panic", "error", r, "path", c.Request.URL.Path, "stack", string(debug.Stack()))
			Reply(c, http.StatusInternalServerError, Text("internal server error"))
			c.Abort()
		}
	}()
	c.Next()
}
