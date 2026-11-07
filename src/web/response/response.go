// Package response is the single shape every HTTP response in an inceptools
// service takes, and the single place an outgoing message is scrubbed.
//
// One envelope, always:
//
//	{ "data": <payload|null>, "meta": { "success": true, "traceId": "...", ... } }
//
// A client written against one endpoint can read every endpoint: success is a
// boolean in a fixed place, pagination is in a fixed place, the trace id to
// quote in a support ticket is in a fixed place. The cost is a wrapper object;
// the benefit is that no consumer ever has to ask what shape THIS route
// returns.
//
// The other reason this package is narrow: Send is the funnel. Every response
// passes through it, so the leak check runs once, here, rather than being
// remembered at several hundred call sites.
package response

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/IncepTools/go/src/core/errs"
)

// Keys under which the middleware stores per-request values on fiber.Ctx.
const (
	LocalTraceID  = "traceId"
	LocalStartKey = "startTime"
)

// Meta is the envelope's second half: everything about the response that is
// not the payload.
type Meta struct {
	Success     bool   `json:"success"`
	TraceID     string `json:"traceId"`
	Message     string `json:"message"`
	RequestTime int64  `json:"requestTime"` // milliseconds spent serving
	StatusCode  int    `json:"statusCode"`
	Route       string `json:"route"`

	// Code is the machine-readable error code, set only on failures that
	// carry one. Clients branch on this, never on Message.
	Code string `json:"code,omitempty"`
	// Errors carries field-scoped detail, set only on failures that have it.
	Errors []errs.Detail `json:"errors,omitempty"`

	// Set only on list responses.
	Page     *int   `json:"page,omitempty"`
	PageSize *int   `json:"pageSize,omitempty"`
	Total    *int64 `json:"total,omitempty"`
	Pages    *int   `json:"pages,omitempty"`
}

// Envelope is the body of every response.
type Envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// Sanitizer inspects an outgoing message and returns what may be sent.
//
// It is the hook for the one thing a response funnel is uniquely able to do:
// catch infrastructure detail on its way out. Handlers pass err.Error() into
// these responders by the hundred, and any one of those strings may have come
// from a database driver, an HTTP client or a message broker. Install a sanitizer
// and the check happens once, for all of them.
//
// route is "METHOD /path"; scope is "http".
type Sanitizer func(c fiber.Ctx, scope, route, message string) string

var sanitizer atomic.Pointer[Sanitizer]

// SetSanitizer installs the outgoing-message filter. Passing nil removes it.
// Call it once, at boot.
func SetSanitizer(s Sanitizer) {
	if s == nil {
		sanitizer.Store(nil)
		return
	}
	sanitizer.Store(&s)
}

func scrub(c fiber.Ctx, message string) string {
	if p := sanitizer.Load(); p != nil {
		return (*p)(c, "http", c.Method()+" "+c.Path(), message)
	}
	return message
}

// genericInternal is what a KindInternal message is replaced with. The real
// message is logged under the trace id; the client gets a sentence and an id
// to quote.
const genericInternal = "an internal error occurred"

func buildMeta(c fiber.Ctx, success bool, message string, status int) Meta {
	traceID, _ := c.Locals(LocalTraceID).(string)
	if traceID == "" {
		traceID = uuid.NewString()
	}
	var elapsed int64
	if start, ok := c.Locals(LocalStartKey).(time.Time); ok {
		elapsed = time.Since(start).Milliseconds()
	}
	return Meta{
		Success:     success,
		TraceID:     traceID,
		Message:     message,
		RequestTime: elapsed,
		StatusCode:  status,
		Route:       c.Path(),
	}
}

// Send writes the envelope. Every other function in this package goes through
// it, and so should any responder you add.
func Send(c fiber.Ctx, status int, message string, data any) error {
	message = scrub(c, message)
	return c.Status(status).JSON(Envelope{Data: data, Meta: buildMeta(c, status < 400, message, status)})
}

// List writes a paginated collection with the page metadata filled in.
func List(c fiber.Ctx, message string, data any, page, pageSize int, total int64) error {
	if pageSize <= 0 {
		pageSize = 1
	}
	pages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		pages++
	}
	meta := buildMeta(c, true, scrub(c, message), fiber.StatusOK)
	meta.Page, meta.PageSize, meta.Total, meta.Pages = &page, &pageSize, &total, &pages
	return c.Status(fiber.StatusOK).JSON(Envelope{Data: data, Meta: meta})
}

// OK writes 200.
func OK(c fiber.Ctx, message string, data any) error {
	return Send(c, fiber.StatusOK, message, data)
}

// Created writes 201.
func Created(c fiber.Ctx, message string, data any) error {
	return Send(c, fiber.StatusCreated, message, data)
}

// Accepted writes 202 — the right code for work that was queued rather than
// done. Provisioning a VM is Accepted, not Created: the resource record exists
// but the thing itself does not yet.
func Accepted(c fiber.Ctx, message string, data any) error {
	return Send(c, fiber.StatusAccepted, message, data)
}

// NoContent writes 204 with no body. It is the one response that is NOT an
// envelope, because 204 means exactly that.
func NoContent(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }

// BadRequest writes 400.
func BadRequest(c fiber.Ctx, message string, data any) error {
	return Send(c, fiber.StatusBadRequest, message, data)
}

// Unauthorized writes 401.
func Unauthorized(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusUnauthorized, message, nil)
}

// Forbidden writes 403.
func Forbidden(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusForbidden, message, nil)
}

// NotFound writes 404.
func NotFound(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusNotFound, message, nil)
}

// Conflict writes 409.
func Conflict(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusConflict, message, nil)
}

// UnprocessableEntity writes 422 — syntactically valid, semantically refused.
func UnprocessableEntity(c fiber.Ctx, message string, data any) error {
	return Send(c, fiber.StatusUnprocessableEntity, message, data)
}

// PaymentRequired writes 402.
func PaymentRequired(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusPaymentRequired, message, nil)
}

// TooManyRequests writes 429.
func TooManyRequests(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusTooManyRequests, message, nil)
}

// InternalError writes 500 with a generic message. The message you pass is
// NOT sent — log it instead; the client gets the trace id.
func InternalError(c fiber.Ctx) error {
	return Send(c, fiber.StatusInternalServerError, genericInternal, nil)
}

// ServiceUnavailable writes 503.
func ServiceUnavailable(c fiber.Ctx, message string) error {
	return Send(c, fiber.StatusServiceUnavailable, message, nil)
}

// StatusFor maps an error kind to an HTTP status. It is exported so a custom
// exception filter can reuse the mapping without re-deriving it.
func StatusFor(kind errs.Kind) int {
	switch kind {
	case errs.KindInvalid:
		return fiber.StatusBadRequest
	case errs.KindUnauthorized:
		return fiber.StatusUnauthorized
	case errs.KindForbidden:
		return fiber.StatusForbidden
	case errs.KindNotFound:
		return fiber.StatusNotFound
	case errs.KindConflict:
		return fiber.StatusConflict
	case errs.KindExhausted:
		return fiber.StatusTooManyRequests
	case errs.KindPayment:
		return fiber.StatusPaymentRequired
	case errs.KindUnavailable:
		return fiber.StatusServiceUnavailable
	case errs.KindTimeout:
		return fiber.StatusGatewayTimeout
	case errs.KindNotSupported:
		return fiber.StatusNotImplemented
	default:
		return fiber.StatusInternalServerError
	}
}

// Fail writes the HTTP response for any error.
//
// This is what a controller returns on the sad path — `return response.Fail(c, err)`
// — and what the exception filter calls for anything that escaped. An
// *errs.Error decides its own status, code and field details. Anything else is
// a 500 whose message is replaced, because an unclassified error is by
// definition one nobody checked for leaks.
func Fail(c fiber.Ctx, err error) error {
	if err == nil {
		return OK(c, "ok", nil)
	}
	e := errs.From(err)
	kind := errs.KindOf(e)
	status := StatusFor(kind)

	message := e.Message
	if kind == errs.KindInternal || message == "" {
		message = genericInternal
	} else {
		message = scrub(c, message)
	}

	meta := buildMeta(c, false, message, status)
	meta.Code = e.Code
	if kind != errs.KindInternal {
		meta.Errors = e.Details
	}
	return c.Status(status).JSON(Envelope{Data: nil, Meta: meta})
}

// FailValidation writes 400 with field-scoped details, the shape a form
// expects.
func FailValidation(c fiber.Ctx, details []errs.Detail) error {
	e := errs.Invalid("validation failed").WithCode("VALIDATION_FAILED").WithDetails(details...)
	return Fail(c, e)
}

// TraceID returns the request's correlation id, generating one if the
// middleware has not run.
func TraceID(c fiber.Ctx) string {
	if id, ok := c.Locals(LocalTraceID).(string); ok && id != "" {
		return id
	}
	return uuid.NewString()
}

// Route renders the handler route for logs: "GET /api/v1/catalog/products".
func Route(c fiber.Ctx) string {
	return strings.TrimSpace(c.Method() + " " + c.Path())
}
