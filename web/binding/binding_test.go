package binding_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/web/binding"
)

type createProduct struct {
	Name     string   `json:"name" validate:"required,min=2,max=20"`
	CIDR     string   `json:"cidr" validate:"required,cidrv4"`
	Zones    []string `json:"zones" validate:"omitempty,min=1,dive,required"`
	Protocol string   `json:"protocol" validate:"omitempty,oneof=tcp udp"`
}

type trimmed struct {
	Host string `json:"host" validate:"required"`
}

func (t *trimmed) Normalize() { t.Host = strings.ToLower(strings.TrimSpace(t.Host)) }

type defaulted struct {
	Mode string `json:"mode" validate:"required,oneof=fast slow"`
}

func (d *defaulted) SetDefaults() {
	if d.Mode == "" {
		d.Mode = "fast"
	}
}

type ranged struct {
	Start int `json:"start" validate:"required"`
	End   int `json:"end" validate:"required"`
}

func (r *ranged) Validate() error {
	if r.End <= r.Start {
		return errs.Invalid("end must be after start").
			WithDetails(errs.Detail{Field: "end", Message: "end must be after start", Rule: "range"})
	}
	return nil
}

func post[T any](t *testing.T, body string) (T, error) {
	t.Helper()
	var (
		got T
		err error
	)
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		got, err = binding.Body[T](c)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if _, terr := app.Test(req); terr != nil {
		t.Fatal(terr)
	}
	return got, err
}

func TestValidRequestBinds(t *testing.T) {
	got, err := post[createProduct](t, `{"name":"web","cidr":"10.0.1.0/24","protocol":"tcp"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "web" || got.CIDR != "10.0.1.0/24" {
		t.Fatalf("got = %+v", got)
	}
}

func TestErrorsUseTheJSONFieldName(t *testing.T) {
	_, err := post[createProduct](t, `{"cidr":"10.0.1.0/24"}`)
	if err == nil {
		t.Fatal("missing required field was accepted")
	}
	d := errs.From(err).Details
	if len(d) == 0 {
		t.Fatal("no details")
	}
	if d[0].Field != "name" {
		t.Fatalf("field = %q, want the JSON name 'name', not the Go field", d[0].Field)
	}
}

func TestMessagesUseTheRightUnit(t *testing.T) {
	_, err := post[createProduct](t, `{"name":"a","cidr":"10.0.1.0/24"}`)
	msg := errs.From(err).Details[0].Message
	if !strings.Contains(msg, "characters") {
		t.Fatalf("message = %q; a string min should say characters", msg)
	}
}

func TestOneOfListsTheOptions(t *testing.T) {
	_, err := post[createProduct](t, `{"name":"web","cidr":"10.0.1.0/24","protocol":"sctp"}`)
	msg := errs.From(err).Details[0].Message
	if !strings.Contains(msg, "tcp") || !strings.Contains(msg, "udp") {
		t.Fatalf("message = %q; it must name the allowed values", msg)
	}
}

func TestMalformedJSONIsAClientError(t *testing.T) {
	_, err := post[createProduct](t, `{"name":`)
	if err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if !errs.Is(err, errs.KindInvalid) {
		t.Fatalf("kind = %q, want invalid — malformed input is a 400, not a 500", errs.KindOf(err))
	}
}

func TestNormalizeRunsBeforeValidation(t *testing.T) {
	got, err := post[trimmed](t, `{"host":"  API.Example.COM  "}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "api.example.com" {
		t.Fatalf("host = %q; Normalize must run and its result must be kept", got.Host)
	}
}

func TestDefaultsCanSatisfyRequired(t *testing.T) {
	got, err := post[defaulted](t, `{}`)
	if err != nil {
		t.Fatalf("a default did not satisfy the required rule: %v", err)
	}
	if got.Mode != "fast" {
		t.Fatalf("mode = %q", got.Mode)
	}
}

func TestSelfValidatorRunsAfterTagRules(t *testing.T) {
	_, err := post[ranged](t, `{"start":10,"end":5}`)
	if err == nil {
		t.Fatal("the cross-field rule did not run")
	}
	d := errs.From(err).Details
	if len(d) == 0 || d[0].Field != "end" {
		t.Fatalf("details = %+v", d)
	}
}

func TestSelfValidatorIsNotReachedWhenFieldsAreInvalid(t *testing.T) {
	// It may assume well-formed fields, so it must not run before they are.
	_, err := post[ranged](t, `{}`)
	d := errs.From(err).Details
	for _, x := range d {
		if x.Rule == "range" {
			t.Fatal("the cross-field rule ran on fields that failed their own rules")
		}
	}
}

func TestNestedFieldPathIsReadable(t *testing.T) {
	type rule struct {
		Protocol string `json:"protocol" validate:"required"`
	}
	type sg struct {
		Rules []rule `json:"rules" validate:"required,dive"`
	}
	_, err := post[sg](t, `{"rules":[{"protocol":""}]}`)
	if err == nil {
		t.Fatal("expected a failure")
	}
	field := errs.From(err).Details[0].Field
	if !strings.Contains(field, "rules") || !strings.Contains(field, "protocol") {
		t.Fatalf("field = %q; it should point at rules[0].protocol", field)
	}
}
