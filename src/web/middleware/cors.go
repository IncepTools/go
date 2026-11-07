package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// CORSOptions configures CORS.
type CORSOptions struct {
	// AllowOrigins is an explicit list. "*" is honoured ONLY when
	// AllowCredentials is false — the combination is forbidden by the spec and
	// browsers reject it, so this middleware refuses to emit it rather than
	// letting you ship a configuration that silently never works.
	AllowOrigins []string
	// AllowOriginFunc decides per request, for the wildcard-subdomain case.
	// It takes precedence over AllowOrigins.
	AllowOriginFunc  func(origin string) bool
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           time.Duration
}

var defaultCORSMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

var defaultCORSHeaders = []string{
	"Origin", "Content-Type", "Accept", "Authorization",
	HeaderRequestID, "X-Idempotency-Key",
}

// CORS answers preflights and sets the cross-origin headers.
//
// It echoes the matched origin rather than sending the configured list, and
// sets Vary: Origin, so a shared cache cannot serve one tenant's origin header
// to another's browser.
func CORS(opt CORSOptions) fiber.Handler {
	methods := strings.Join(orDefault(opt.AllowMethods, defaultCORSMethods), ", ")
	headers := strings.Join(orDefault(opt.AllowHeaders, defaultCORSHeaders), ", ")
	expose := strings.Join(orDefault(opt.ExposeHeaders, []string{HeaderRequestID, HeaderTraceID}), ", ")
	maxAge := strconv.Itoa(int(opt.MaxAge.Seconds()))

	wildcard := !opt.AllowCredentials && contains(opt.AllowOrigins, "*")

	allowed := func(origin string) bool {
		if origin == "" {
			return false
		}
		if opt.AllowOriginFunc != nil {
			return opt.AllowOriginFunc(origin)
		}
		if wildcard {
			return true
		}
		return contains(opt.AllowOrigins, origin)
	}

	return func(c fiber.Ctx) error {
		origin := c.Get("Origin")
		// Vary goes on every response, matched or not: a cache keyed without
		// it will serve a no-CORS response to a browser that needed one.
		c.Set("Vary", "Origin")

		if !allowed(origin) {
			if c.Method() == fiber.MethodOptions {
				return c.SendStatus(fiber.StatusNoContent)
			}
			return c.Next()
		}

		if wildcard {
			c.Set("Access-Control-Allow-Origin", "*")
		} else {
			c.Set("Access-Control-Allow-Origin", origin)
		}
		if opt.AllowCredentials {
			c.Set("Access-Control-Allow-Credentials", "true")
		}
		if expose != "" {
			c.Set("Access-Control-Expose-Headers", expose)
		}

		if c.Method() == fiber.MethodOptions {
			c.Set("Access-Control-Allow-Methods", methods)
			c.Set("Access-Control-Allow-Headers", headers)
			if opt.MaxAge > 0 {
				c.Set("Access-Control-Max-Age", maxAge)
			}
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Next()
	}
}

func orDefault(v, def []string) []string {
	if len(v) == 0 {
		return def
	}
	return v
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
