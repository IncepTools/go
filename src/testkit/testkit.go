// Package testkit makes a module testable without a server, a port or a
// sleep.
//
// An application assembled in memory, exercised through Fiber's own test
// transport, with the response envelope decoded for you.
//
// The point is that these are not unit tests with the HTTP layer mocked away.
// They go through the real middleware, the real guards and the real exception
// filter, which is where most of the behaviour this framework provides
// actually lives — and where a mock would quietly diverge.
package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/response"
)

// Harness is an application under test.
type Harness struct {
	T   *testing.T
	App *fiber.App
	// Headers are sent on every request — a bearer token, a tenant header.
	Headers map[string]string
}

// Response is a decoded reply.
type Response struct {
	Status int
	Body   []byte
	Meta   response.Meta
	// Data is the raw payload, still encoded, so a test can decode it into
	// whatever type that endpoint returns.
	Data json.RawMessage
}

// JSON decodes the payload into dst.
func (r Response) JSON(t *testing.T, dst any) {
	t.Helper()
	if len(r.Data) == 0 {
		t.Fatalf("response has no data: %s", string(r.Body))
	}
	if err := json.Unmarshal(r.Data, dst); err != nil {
		t.Fatalf("decoding response data: %v\nbody: %s", err, string(r.Body))
	}
}

// ExpectStatus fails the test unless the status matches, printing the body —
// which is what you actually want to see when it does not.
func (r Response) ExpectStatus(t *testing.T, want int) Response {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status = %d, want %d\nbody: %s", r.Status, want, string(r.Body))
	}
	return r
}

// ExpectCode fails the test unless meta.code matches.
func (r Response) ExpectCode(t *testing.T, want string) Response {
	t.Helper()
	if r.Meta.Code != want {
		t.Fatalf("error code = %q, want %q\nbody: %s", r.Meta.Code, want, string(r.Body))
	}
	return r
}

// ExpectFieldError fails the test unless a validation detail names field.
func (r Response) ExpectFieldError(t *testing.T, field string) Response {
	t.Helper()
	for _, d := range r.Meta.Errors {
		if d.Field == field {
			return r
		}
	}
	t.Fatalf("no validation error for field %q\nbody: %s", field, string(r.Body))
	return r
}

// Errors returns the field-scoped details.
func (r Response) Errors() []errs.Detail { return r.Meta.Errors }

// New builds a harness around a Fiber app.
func New(t *testing.T, app *fiber.App) *Harness {
	t.Helper()
	return &Harness{T: t, App: app, Headers: map[string]string{}}
}

// Mount builds a harness that serves the given apps under prefix, with no
// middleware beyond what the modules declare.
//
// Use it for a module test. For a test of the whole service, build the real
// inceptools.Application and hand its Fiber app to New — otherwise the test
// passes through a pipeline the production one does not have.
func Mount(t *testing.T, prefix string, apps ...module.App) *Harness {
	t.Helper()
	f := fiber.New()
	r := fiber.Router(f)
	if prefix != "" {
		r = f.Group(prefix)
	}
	for _, a := range apps {
		if err := module.MountApp(r, a, module.MountOptions{Prefix: prefix}); err != nil {
			t.Fatalf("mounting %s: %v", a.Name(), err)
		}
	}
	return New(t, f)
}

// WithHeader adds a header to every subsequent request.
func (h *Harness) WithHeader(key, value string) *Harness {
	h.Headers[key] = value
	return h
}

// Do sends a request and decodes the envelope.
func (h *Harness) Do(method, path string, body any) Response {
	h.T.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.T.Fatalf("encoding request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}

	// A generous timeout: slow enough never to flake on a loaded CI runner,
	// short enough that a deadlocked handler fails the test instead of
	// hanging the suite.
	res, err := h.App.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		h.T.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(res.Body)
	out := Response{Status: res.StatusCode, Body: raw}

	var env struct {
		Data json.RawMessage `json:"data"`
		Meta response.Meta   `json:"meta"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &env) == nil {
		out.Data, out.Meta = env.Data, env.Meta
	}
	return out
}

// Get sends a GET.
func (h *Harness) Get(path string) Response { return h.Do(http.MethodGet, path, nil) }

// Post sends a POST with a JSON body.
func (h *Harness) Post(path string, body any) Response { return h.Do(http.MethodPost, path, body) }

// Put sends a PUT with a JSON body.
func (h *Harness) Put(path string, body any) Response { return h.Do(http.MethodPut, path, body) }

// Patch sends a PATCH with a JSON body.
func (h *Harness) Patch(path string, body any) Response { return h.Do(http.MethodPatch, path, body) }

// Delete sends a DELETE.
func (h *Harness) Delete(path string) Response { return h.Do(http.MethodDelete, path, nil) }

// Context returns a context carrying a tenant and an actor, for testing a
// service or a repository directly.
func Context(account, user uuid.UUID, roles ...string) context.Context {
	ctx := ctxkit.WithTenant(context.Background(), ctxkit.Tenant{AccountID: account})
	ctx = ctxkit.WithActor(ctx, ctxkit.Actor{UserID: user, Roles: roles})
	return ctxkit.WithTrace(ctx, "test-"+uuid.NewString())
}

// OperatorContext returns a platform-operator context.
func OperatorContext(user uuid.UUID) context.Context {
	ctx := ctxkit.WithTenant(context.Background(), ctxkit.Tenant{AccountID: uuid.New(), Operator: true})
	ctx = ctxkit.WithActor(ctx, ctxkit.Actor{UserID: user})
	return ctxkit.WithTrace(ctx, "test-"+uuid.NewString())
}
