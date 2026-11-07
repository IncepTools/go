// Package binding turns a request into a validated DTO, or into an error that
// already knows how to render itself.
//
// The generic form means a controller never declares a variable just to pass
// its address somewhere:
//
//	req, err := binding.Body[dto.CreateProduct](c)
//	if err != nil {
//	    return response.Fail(c, err)
//	}
//
// Three conventions the package enforces, each because its absence has cost
// somebody a production incident:
//
//   - A DTO with no `validate` tag on a required field is accepted silently by
//     most validators. Use `required` and mean it; Strict mode below makes the
//     omission visible.
//   - A PATCH field that may legitimately be false or zero is a POINTER. A
//     plain bool cannot distinguish "set it to false" from "do not touch it",
//     and the second is what a rename request means.
//   - Validation failures come back as field-scoped details, never as a single
//     prose sentence a form cannot attach to an input.
package binding

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/core/errs"
)

// Defaulter is implemented by a DTO that fills its own optional fields before
// validation. It runs after binding and before the rules, so a default can
// satisfy a `required` rule.
type Defaulter interface{ SetDefaults() }

// Normalizer is implemented by a DTO that cleans its input — trimming
// whitespace, lowercasing a hostname, stripping a trailing dot. It runs before
// validation, so the rules see the value that will actually be stored.
type Normalizer interface{ Normalize() }

// SelfValidator is implemented by a DTO with a rule that tags cannot express:
// "end must be after start", "exactly one of these three". It runs AFTER tag
// validation, so it can assume the individual fields are well-formed.
//
// Return an *errs.Error with details to attach the failure to a field; a plain
// error becomes a form-level message.
type SelfValidator interface{ Validate() error }

var (
	mu       sync.RWMutex
	validate = newValidator()
	messages = map[string]func(validator.FieldError) string{}
)

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report the JSON name, not the Go field name. A client that sent
	// "category_id" must be told "category_id is required", not "CategoryID".
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return v
}

// Validator exposes the underlying instance so an application can register
// struct-level rules or custom types.
func Validator() *validator.Validate {
	mu.RLock()
	defer mu.RUnlock()
	return validate
}

// RegisterRule adds a custom `validate` tag, usable as validate:"<tag>".
//
//	binding.RegisterRule("cidr4", func(fl validator.FieldLevel) bool { ... })
func RegisterRule(tag string, fn validator.Func, callEvenIfNull ...bool) error {
	mu.Lock()
	defer mu.Unlock()
	return validate.RegisterValidation(tag, fn, callEvenIfNull...)
}

// RegisterMessage sets the human sentence produced for a tag. Without one, a
// custom rule renders as "<field> is invalid", which tells a user nothing
// about how to fix it.
func RegisterMessage(tag string, fn func(validator.FieldError) string) {
	mu.Lock()
	defer mu.Unlock()
	messages[tag] = fn
}

// Validate runs the rules on v and returns one Detail per failing field.
// A nil return means valid.
func Validate(v any) []errs.Detail {
	if n, ok := v.(Normalizer); ok {
		n.Normalize()
	}
	if d, ok := v.(Defaulter); ok {
		d.SetDefaults()
	}

	mu.RLock()
	val := validate
	mu.RUnlock()

	var details []errs.Detail
	if err := val.Struct(v); err != nil {
		var invalid *validator.InvalidValidationError
		if ok := asInvalid(err, &invalid); ok {
			return []errs.Detail{{Field: "body", Message: "request body is not an object", Rule: "invalid"}}
		}
		var fieldErrs validator.ValidationErrors
		if ok := asFieldErrors(err, &fieldErrs); ok {
			for _, fe := range fieldErrs {
				details = append(details, errs.Detail{
					Field:   fieldPath(fe),
					Message: message(fe),
					Rule:    fe.Tag(),
				})
			}
		} else {
			return []errs.Detail{{Field: "body", Message: err.Error(), Rule: "invalid"}}
		}
	}
	if len(details) > 0 {
		return details
	}

	if sv, ok := v.(SelfValidator); ok {
		if err := sv.Validate(); err != nil {
			e := errs.From(err)
			if len(e.Details) > 0 {
				return e.Details
			}
			msg := e.Message
			if msg == "" {
				msg = err.Error()
			}
			return []errs.Detail{{Field: "body", Message: msg, Rule: "rule"}}
		}
	}
	return nil
}

func asInvalid(err error, target **validator.InvalidValidationError) bool {
	return errors.As(err, target)
}

func asFieldErrors(err error, target *validator.ValidationErrors) bool {
	return errors.As(err, target)
}

// fieldPath renders a nested field the way the client sent it:
// "rules[0].protocol" rather than "CreateRequest.Rules[0].Protocol".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.Index(ns, "."); i >= 0 {
		ns = ns[i+1:]
	}
	return ns
}

func message(fe validator.FieldError) string {
	mu.RLock()
	custom := messages[fe.Tag()]
	mu.RUnlock()
	if custom != nil {
		return custom(fe)
	}

	name := fieldPath(fe)
	switch fe.Tag() {
	case "required":
		return name + " is required"
	case "required_if", "required_unless", "required_with", "required_without":
		return name + " is required here"
	case "email":
		return name + " must be a valid email address"
	case "url", "uri":
		return name + " must be a valid URL"
	case "uuid", "uuid4":
		return name + " must be a valid UUID"
	case "ip", "ipv4", "ipv6":
		return name + " must be a valid IP address"
	case "cidr", "cidrv4", "cidrv6":
		return name + " must be a valid CIDR block"
	case "hostname", "hostname_rfc1123", "fqdn":
		return name + " must be a valid hostname"
	case "min":
		return name + " must be " + quantity(fe, "at least")
	case "max":
		return name + " must be " + quantity(fe, "at most")
	case "len":
		return name + " must be " + quantity(fe, "exactly")
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", name, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be %s or more", name, fe.Param())
	case "lt":
		return fmt.Sprintf("%s must be less than %s", name, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be %s or less", name, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", name, strings.ReplaceAll(fe.Param(), " ", ", "))
	case "eqfield", "eqcsfield":
		return fmt.Sprintf("%s must match %s", name, fe.Param())
	case "alphanum":
		return name + " must contain only letters and digits"
	case "numeric":
		return name + " must be a number"
	case "boolean":
		return name + " must be true or false"
	case "json":
		return name + " must be valid JSON"
	case "base64":
		return name + " must be base64"
	case "dive":
		return name + " contains an invalid entry"
	default:
		return name + " is invalid"
	}
}

// quantity renders a min/max/len message in the unit the rule actually uses:
// characters for a string, entries for a collection, otherwise a bare number.
// "name must be at least 2" is a worse message than "name must be at least 2
// characters", and "tag_ids must be at least 1 characters" is just wrong.
func quantity(fe validator.FieldError, qualifier string) string {
	unit := ""
	switch fe.Kind() {
	case reflect.String:
		unit = " characters"
	case reflect.Slice, reflect.Array, reflect.Map:
		unit = " entries"
	}
	return qualifier + " " + fe.Param() + unit
}

// Body binds and validates a JSON request body.
func Body[T any](c fiber.Ctx) (T, error) {
	var dst T
	if err := c.Bind().Body(&dst); err != nil {
		return dst, errs.Invalid("request body could not be read").
			WithCode("MALFORMED_BODY").
			WithCause(err).
			WithDetails(errs.Detail{Field: "body", Message: "body is not valid JSON", Rule: "parse"})
	}
	if err := finish(&dst); err != nil {
		return dst, err
	}
	return dst, nil
}

// Query binds and validates the query string.
func Query[T any](c fiber.Ctx) (T, error) {
	var dst T
	if err := c.Bind().Query(&dst); err != nil {
		return dst, errs.Invalid("query parameters could not be read").
			WithCode("MALFORMED_QUERY").
			WithCause(err)
	}
	if err := finish(&dst); err != nil {
		return dst, err
	}
	return dst, nil
}

// Params binds and validates the path parameters.
func Params[T any](c fiber.Ctx) (T, error) {
	var dst T
	if err := c.Bind().URI(&dst); err != nil {
		return dst, errs.Invalid("path parameters could not be read").
			WithCode("MALFORMED_PARAMS").
			WithCause(err)
	}
	if err := finish(&dst); err != nil {
		return dst, err
	}
	return dst, nil
}

// Header binds and validates request headers.
func Header[T any](c fiber.Ctx) (T, error) {
	var dst T
	if err := c.Bind().Header(&dst); err != nil {
		return dst, errs.Invalid("headers could not be read").WithCode("MALFORMED_HEADERS").WithCause(err)
	}
	if err := finish(&dst); err != nil {
		return dst, err
	}
	return dst, nil
}

// finish normalises, defaults and validates in place.
//
// It takes a POINTER, and every caller passes the address of its own local —
// never a copy. Normalize and SetDefaults mutate the value, and a copy would
// throw those mutations away while still reporting success, which is the
// quietest possible way for a default never to be applied.
func finish(dst any) error {
	if details := Validate(dst); len(details) > 0 {
		return errs.Invalid("validation failed").WithCode("VALIDATION_FAILED").WithDetails(details...)
	}
	return nil
}

// Into binds and validates into an existing pointer, for the rare case where
// the DTO type is not known statically.
func Into(c fiber.Ctx, dst any) error {
	if err := c.Bind().Body(dst); err != nil {
		return errs.Invalid("request body could not be read").WithCode("MALFORMED_BODY").WithCause(err)
	}
	if details := Validate(dst); len(details) > 0 {
		return errs.Invalid("validation failed").WithCode("VALIDATION_FAILED").WithDetails(details...)
	}
	return nil
}
