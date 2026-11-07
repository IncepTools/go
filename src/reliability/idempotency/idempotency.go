// Package idempotency makes a repeated POST safe.
//
// A client whose request times out does not know whether it happened. It
// retries. Without this, the retry provisions a second VM and bills for it —
// and the client still does not know, so it retries again.
//
// The contract, which matches Stripe's and is what API clients already expect:
// send an Idempotency-Key header; the first request executes and its response
// is recorded against the key; a later request with the same key returns that
// recorded response without executing anything.
//
// Two details that are easy to get wrong and expensive to get wrong:
//
//   - The key is scoped to the TENANT, the method and the path. A global key
//     space lets one account's key collide with another's and serve them
//     someone else's response body.
//   - The request fingerprint is stored and compared. Reusing a key with a
//     different body is a client bug; returning the old response for it would
//     hide that bug, so it is refused with 422.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/response"
)

// HeaderKey is the request header carrying the key.
const HeaderKey = "Idempotency-Key"

// HeaderReplayed is set on a response served from the record, so a client can
// tell a replay from a fresh execution.
const HeaderReplayed = "Idempotent-Replayed"

// ErrInFlight is returned by a store when the first request with this key has
// not finished yet.
var ErrInFlight = errors.New("idempotency: the original request is still running")

// ErrNotFound is returned by a store when the key is unknown.
var ErrNotFound = errors.New("idempotency: no record")

// Record is a completed response, kept so it can be replayed.
type Record struct {
	Key         string
	Fingerprint string
	Status      int
	Body        []byte
	ContentType string
	CreatedAt   time.Time
}

// Store persists records.
//
// Begin MUST be atomic: it either reserves the key for this caller or reports
// that somebody already has it. A read-then-write implementation reintroduces
// exactly the race this package exists to close.
type Store interface {
	// Begin reserves key. It returns the existing record on a replay,
	// ErrInFlight when the original is still running, or (nil, nil) when this
	// caller now owns the key.
	Begin(ctx context.Context, key, fingerprint string, ttl time.Duration) (*Record, error)
	// Complete records the response.
	Complete(ctx context.Context, key string, rec Record, ttl time.Duration) error
	// Abandon releases a reservation whose request failed to produce a
	// recordable response, so the client may retry.
	Abandon(ctx context.Context, key string) error
}

// Options configures the middleware.
type Options struct {
	Store Store
	// TTL is how long a record is replayable. Default 24h — long enough to
	// cover a client's whole retry schedule, short enough that the table does
	// not grow forever.
	TTL time.Duration
	// Methods that require a key. Default: POST, PATCH, DELETE.
	//
	// PUT is absent because a correct PUT is already idempotent. GET is
	// absent because it changes nothing.
	Methods []string
	// Require makes a missing key a 400 on those methods. Off by default:
	// turning it on breaks every existing client at once. Turn it on for a
	// new API.
	Require bool
	// Skip exempts a request.
	Skip func(c fiber.Ctx) bool
}

// New returns the middleware.
func New(opt Options) fiber.Handler {
	if opt.Store == nil {
		panic("idempotency: no Store configured")
	}
	if opt.TTL <= 0 {
		opt.TTL = 24 * time.Hour
	}
	if len(opt.Methods) == 0 {
		opt.Methods = []string{http.MethodPost, http.MethodPatch, http.MethodDelete}
	}
	methods := map[string]bool{}
	for _, m := range opt.Methods {
		methods[m] = true
	}

	return func(c fiber.Ctx) error {
		if !methods[c.Method()] || (opt.Skip != nil && opt.Skip(c)) {
			return c.Next()
		}

		key := c.Get(HeaderKey)
		if key == "" {
			if opt.Require {
				return response.Fail(c, errs.Invalid("the %s header is required on this request", HeaderKey).
					WithCode("IDEMPOTENCY_KEY_REQUIRED"))
			}
			return c.Next()
		}
		if len(key) > 255 {
			return response.Fail(c, errs.Invalid("%s must be at most 255 characters", HeaderKey).
				WithCode("IDEMPOTENCY_KEY_TOO_LONG"))
		}

		scoped := scope(c, key)
		fp := fingerprint(c)

		existing, err := opt.Store.Begin(c.Context(), scoped, fp, opt.TTL)
		switch {
		case errors.Is(err, ErrInFlight):
			// 409, not a wait: the client is retrying because it thinks the
			// first call is lost, and blocking here just doubles its timeout.
			return response.Fail(c, errs.Conflict("a request with this %s is still in progress", HeaderKey).
				WithCode("IDEMPOTENCY_IN_FLIGHT"))
		case err != nil:
			// Fail open. An idempotency store that is down must not take
			// writes down with it — the cost is that a retry in that window
			// may duplicate, which is the behaviour without this middleware
			// at all.
			logx.C(c.Context()).Error("idempotency store failed; proceeding unprotected",
				"error", err.Error(), "route", c.Path())
			return c.Next()
		}

		if existing != nil {
			if existing.Fingerprint != fp {
				return response.Fail(c, errs.Invalid(
					"this %s was already used with a different request body", HeaderKey).
					WithCode("IDEMPOTENCY_KEY_REUSED"))
			}
			c.Set(HeaderReplayed, "true")
			if existing.ContentType != "" {
				c.Set(fiber.HeaderContentType, existing.ContentType)
			}
			return c.Status(existing.Status).Send(existing.Body)
		}

		err = c.Next()

		status := c.Response().StatusCode()
		// Only a definitive outcome is recorded. Replaying a 500 would pin a
		// transient failure to the key for a day, and the client could never
		// succeed with that key again.
		if err != nil || status >= 500 {
			if aerr := opt.Store.Abandon(context.WithoutCancel(c.Context()), scoped); aerr != nil {
				logx.C(c.Context()).Warn("could not release the idempotency key",
					"error", aerr.Error())
			}
			return err
		}

		body := append([]byte(nil), c.Response().Body()...)
		rec := Record{
			Key:         scoped,
			Fingerprint: fp,
			Status:      status,
			Body:        body,
			ContentType: string(c.Response().Header.ContentType()),
			CreatedAt:   time.Now().UTC(),
		}
		if cerr := opt.Store.Complete(context.WithoutCancel(c.Context()), scoped, rec, opt.TTL); cerr != nil {
			logx.C(c.Context()).Warn("could not record the idempotent response", "error", cerr.Error())
		}
		return nil
	}
}

// scope namespaces the key by tenant, method and path, so the same key used
// on two endpoints — or by two accounts — cannot collide.
func scope(c fiber.Ctx, key string) string {
	account := "anon"
	if t := ctxkit.TenantOf(c.Context()); !t.Zero() {
		account = t.AccountID.String()
	}
	return account + "|" + c.Method() + "|" + c.Path() + "|" + key
}

// fingerprint hashes the request body so a reused key with a changed payload
// is caught instead of silently replaying the old response.
func fingerprint(c fiber.Ctx) string {
	h := sha256.Sum256(c.Body())
	return hex.EncodeToString(h[:])
}
