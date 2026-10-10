package core

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"prea/common"

	"github.com/gin-gonic/gin"
)

// Security configures the HTTP protections. They run before every other
// middleware, so a preflight or a blocked request never reaches them.
// Order: Headers, CORS, CSRF, then Middlewares.
type Security struct {
	CORS           *CORS             // nil: no CORS headers, browsers block cross-origin calls
	CSRF           *CSRF             // nil: no cross-origin request protection
	Headers        map[string]string // set on every response
	TrustedProxies []string          // nil: trust no proxy, ClientIP is the peer address
	Middlewares    []Middleware      // your own protections: rate limit, CSRF token, IP allowlist...
}

type CORS struct {
	AllowOrigins     []string                 // "*" allows any origin
	AllowOriginFunc  func(origin string) bool // custom rule, e.g. subdomains; checked after AllowOrigins
	AllowMethods     []string                 // default: GET, POST, PUT, PATCH, DELETE
	AllowHeaders     []string                 // default: Authorization, Content-Type
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// CSRF rejects unsafe cross-origin browser requests (net/http.CrossOriginProtection).
// Requests without Sec-Fetch-Site and Origin, like curl or server-to-server, pass.
// Inside Security, the CORS origins are trusted too.
type CSRF struct {
	TrustedOrigins    []string
	TrustedOriginFunc func(origin string) bool // custom rule, checked when the origin is not in TrustedOrigins
	Bypass            []string                 // http.ServeMux patterns skipped by the check, e.g. "POST /webhooks/"
	Deny              Middleware               // replaces the default 403 response; must abort
}

var DefaultHeaders = map[string]string{
	"X-Content-Type-Options":     "nosniff",
	"X-Frame-Options":            "DENY",
	"Referrer-Policy":            "strict-origin-when-cross-origin",
	"Content-Security-Policy":    "default-src 'none'; frame-ancestors 'none'",
	"Cross-Origin-Opener-Policy": "same-origin",
	"Strict-Transport-Security":  "max-age=63072000; includeSubDomains",
}

func (s Security) apply(engine *gin.Engine) error {
	if err := engine.SetTrustedProxies(s.TrustedProxies); err != nil {
		return fmt.Errorf("security: trusted proxies: %w", err)
	}
	if len(s.Headers) > 0 {
		engine.Use(Headers(s.Headers))
	}
	if s.CORS != nil {
		cors, err := s.CORS.Middleware()
		if err != nil {
			return err
		}
		engine.Use(cors)
	}
	if s.CSRF != nil {
		csrf := *s.CSRF
		if s.CORS != nil {
			csrf.TrustedOrigins = slices.Concat(csrf.TrustedOrigins, s.CORS.AllowOrigins)
			csrf.TrustedOriginFunc = anyOf(csrf.TrustedOriginFunc, s.CORS.AllowOriginFunc)
		}
		mw, err := csrf.Middleware()
		if err != nil {
			return err
		}
		engine.Use(mw)
	}
	engine.Use(s.Middlewares...)
	return nil
}

// Headers sets the given headers on every response. Usable per module or route.
func Headers(values map[string]string) Middleware {
	values = maps.Clone(values)
	return func(c *gin.Context) {
		h := c.Writer.Header()
		for k, v := range values {
			h.Set(k, v)
		}
		c.Next()
	}
}

// Middleware builds the CORS middleware, for use outside Security (per module or route).
func (c CORS) Middleware() (Middleware, error) {
	cfg := c
	cfg.AllowOrigins = slices.Clone(c.AllowOrigins)
	anyOrigin := slices.Contains(cfg.AllowOrigins, "*")
	if anyOrigin && cfg.AllowCredentials {
		return nil, errors.New(`security: CORS AllowCredentials cannot be used with origin "*"`)
	}
	methods := join(cfg.AllowMethods, "GET, POST, PUT, PATCH, DELETE")
	allowHeaders := join(cfg.AllowHeaders, "Authorization, Content-Type")
	expose := join(cfg.ExposeHeaders, "")
	allowed := func(origin string) bool {
		return anyOrigin || slices.Contains(cfg.AllowOrigins, origin) || cfg.AllowOriginFunc != nil && cfg.AllowOriginFunc(origin)
	}

	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		origin := c.GetHeader("Origin")
		if origin == "" || !allowed(origin) {
			c.Next()
			return
		}

		if anyOrigin {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Set("Access-Control-Allow-Origin", origin)
		}
		if cfg.AllowCredentials {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		if expose != "" {
			h.Set("Access-Control-Expose-Headers", expose)
		}

		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", allowHeaders)
			if cfg.MaxAge > 0 {
				h.Set("Access-Control-Max-Age", strconv.Itoa(int(cfg.MaxAge.Seconds())))
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}, nil
}

// Middleware builds the CSRF middleware, for use outside Security (per module or route).
func (cfg CSRF) Middleware() (Middleware, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range cfg.TrustedOrigins {
		if origin == "*" {
			continue
		}
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("security: CSRF: %w", err)
		}
	}
	for _, pattern := range cfg.Bypass {
		protection.AddInsecureBypassPattern(pattern)
	}
	deny := cfg.Deny
	if deny == nil {
		deny = func(c *gin.Context) {
			Error(c, fmt.Errorf("%w: cross-origin request", common.ErrForbidden))
			c.Abort()
		}
	}

	return func(c *gin.Context) {
		if err := protection.Check(c.Request); err != nil {
			if origin := c.GetHeader("Origin"); origin == "" || cfg.TrustedOriginFunc == nil || !cfg.TrustedOriginFunc(origin) {
				deny(c)
				return
			}
		}
		c.Next()
	}, nil
}

func anyOf(fns ...func(string) bool) func(string) bool {
	fns = slices.DeleteFunc(fns, func(fn func(string) bool) bool { return fn == nil })
	if len(fns) == 0 {
		return nil
	}
	return func(origin string) bool {
		return slices.ContainsFunc(fns, func(fn func(string) bool) bool { return fn(origin) })
	}
}

func join(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return strings.Join(values, ", ")
}
