// Package pipe reads one value out of a request and converts it, returning an
// error that already carries the right status and the right field name.
//
// It exists because the alternative — `uuid.Parse(c.Params("id"))` with a
// hand-written error beside it — is written slightly differently at every
// call site, and some of those forget the error entirely and pass a zero
// UUID into a query.
package pipe

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/IncepTools/go/src/core/errs"
)

func invalid(field, message, rule string) error {
	return errs.Invalid("%s", message).
		WithCode("INVALID_PARAMETER").
		WithDetails(errs.Detail{Field: field, Message: message, Rule: rule})
}

// UUIDParam reads a path parameter as a UUID.
func UUIDParam(c fiber.Ctx, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Params(name))
	if raw == "" {
		return uuid.Nil, invalid(name, name+" is required", "required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, invalid(name, name+" must be a valid UUID", "uuid")
	}
	return id, nil
}

// UUIDQuery reads an OPTIONAL query parameter as a UUID. A missing value
// returns uuid.Nil and no error; a present but malformed one is an error.
//
// The distinction matters: `?category_id=` meaning "every category" and `?category_id=oops`
// meaning "a typo" must not behave the same way.
func UUIDQuery(c fiber.Ctx, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, invalid(name, name+" must be a valid UUID", "uuid")
	}
	return id, nil
}

// StringParam reads a required, trimmed path parameter.
func StringParam(c fiber.Ctx, name string) (string, error) {
	raw := strings.TrimSpace(c.Params(name))
	if raw == "" {
		return "", invalid(name, name+" is required", "required")
	}
	return raw, nil
}

// IntParam reads a path parameter as an int.
func IntParam(c fiber.Ctx, name string) (int, error) {
	raw := strings.TrimSpace(c.Params(name))
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalid(name, name+" must be a whole number", "numeric")
	}
	return n, nil
}

// IntQuery reads an optional query parameter as an int, falling back to def.
func IntQuery(c fiber.Ctx, name string, def int) (int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def, invalid(name, name+" must be a whole number", "numeric")
	}
	return n, nil
}

// BoolQuery reads an optional query parameter as a bool, falling back to def.
// It accepts the forms a browser and a CLI actually send: true/false, 1/0,
// yes/no, on/off.
func BoolQuery(c fiber.Ctx, name string, def bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(c.Query(name)))
	switch raw {
	case "":
		return def, nil
	case "1", "t", "true", "yes", "y", "on":
		return true, nil
	case "0", "f", "false", "no", "n", "off":
		return false, nil
	default:
		return def, invalid(name, name+" must be true or false", "boolean")
	}
}

// EnumQuery reads an optional query parameter constrained to a fixed set.
// An empty value returns "" and no error, so the caller decides the default.
func EnumQuery(c fiber.Ctx, name string, allowed ...string) (string, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return "", nil
	}
	for _, a := range allowed {
		if raw == a {
			return raw, nil
		}
	}
	return "", invalid(name, name+" must be one of: "+strings.Join(allowed, ", "), "oneof")
}

// TimeQuery reads an optional RFC 3339 timestamp.
func TimeQuery(c fiber.Ctx, name string) (time.Time, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, invalid(name, name+" must be an RFC 3339 timestamp", "datetime")
	}
	return t, nil
}

// CSVQuery reads an optional comma-separated list, trimmed, with empties
// dropped.
func CSVQuery(c fiber.Ctx, name string) []string {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
