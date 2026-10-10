package core

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine, _, err := App{
		Modules: []*Module{{Name: "m", Providers: []any{routes{path: "/x"}}}},
		Security: Security{
			CORS:    &CORS{AllowOrigins: []string{"http://app.test"}},
			CSRF:    &CSRF{},
			Headers: DefaultHeaders,
		},
	}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	engine.POST("/x", func(c *gin.Context) { c.String(200, "ok") })

	do := func(method, origin, site string, extra ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://api.test/x", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		for i := 0; i < len(extra); i += 2 {
			r.Header.Set(extra[i], extra[i+1])
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}

	w := do("OPTIONS", "http://app.test", "cross-site", "Access-Control-Request-Method", "POST")
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "http://app.test" || w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w := do("GET", "http://evil.test", "cross-site"); w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unknown origin gets no CORS headers: %d %v", w.Code, w.Header())
	}
	if w := do("POST", "http://evil.test", "cross-site"); w.Code != 403 {
		t.Fatalf("cross-site POST must be blocked: %d", w.Code)
	}
	if w := do("POST", "http://app.test", "cross-site"); w.Code != 200 {
		t.Fatalf("CORS origins are trusted by CSRF: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "", ""); w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("non-browser request with headers: %d %v", w.Code, w.Header())
	}

	if _, _, err := (App{Security: Security{CORS: &CORS{AllowOrigins: []string{"*"}, AllowCredentials: true}}}).Build(t.Context()); err == nil {
		t.Fatal("wildcard origin with credentials must fail")
	}
}

func TestSecurityCustom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	subdomain := func(origin string) bool { return strings.HasSuffix(origin, ".app.test") }
	engine, _, err := App{
		Security: Security{
			CORS: &CORS{AllowOriginFunc: subdomain},
			CSRF: &CSRF{Deny: func(c *gin.Context) { c.AbortWithStatus(418) }},
			Middlewares: []Middleware{func(c *gin.Context) {
				if c.GetHeader("X-Blocked") != "" {
					c.AbortWithStatus(429)
					return
				}
				c.Next()
			}},
		},
		Middlewares: []Middleware{trace("global")},
	}.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	engine.POST("/x", func(c *gin.Context) { c.String(200, "ok") })

	post := func(origin string, header ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://api.test/x", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		if len(header) > 0 {
			r.Header.Set(header[0], "1")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}

	if w := post("https://a.app.test"); w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://a.app.test" {
		t.Fatalf("origin func allows CORS and CSRF: %d %v", w.Code, w.Header())
	}
	if w := post("https://evil.test"); w.Code != 418 {
		t.Fatalf("custom deny: %d", w.Code)
	}
	if w := post("https://a.app.test", "X-Blocked"); w.Code != 429 || w.Header().Get("X-Trace") != "" {
		t.Fatalf("custom security middleware runs before globals: %d %v", w.Code, w.Header())
	}

	route, err := (&CORS{AllowOrigins: []string{"https://only.test"}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}
	engine.GET("/y", route, func(c *gin.Context) { c.String(200, "ok") })
	r := httptest.NewRequest("GET", "/y", nil)
	r.Header.Set("Origin", "https://only.test")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://only.test" {
		t.Fatalf("per-route CORS: %v", w.Header())
	}
}
