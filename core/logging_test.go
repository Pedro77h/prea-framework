package core

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"prea/logger"

	"github.com/gin-gonic/gin"
)

func TestRequestLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	logger.Log = logger.New(&buf, logger.LevelDebug, false)

	engine := gin.New()
	engine.Use(RequestLogger, Recovery)
	engine.GET("/ok", func(c *gin.Context) { c.Status(200) })
	engine.GET("/missing", func(c *gin.Context) { Error(c, errors.New("db down")) })
	engine.GET("/panic", func(c *gin.Context) { panic("boom") })

	for path, want := range map[string]string{
		"/ok":      "level=SUCCESS msg=request method=GET path=/ok status=200",
		"/missing": `status=500`,
		"/panic":   `level=ERROR msg=panic error=boom path=/panic`,
		"/nope":    "level=INFO msg=request method=GET path=/nope status=404",
	} {
		buf.Reset()
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(buf.String(), want) {
			t.Errorf("%s: want %q in\n%s", path, want, buf.String())
		}
	}

	buf.Reset()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/missing", nil))
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "db down") {
		t.Fatalf("5xx logs the real error:\n%s", buf.String())
	}
}
