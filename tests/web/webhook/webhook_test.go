package webhook_test

import (
	"github.com/IncepTools/go/src/web/webhook"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	body := []byte(`{"action":"push"}`)
	sig := webhook.Sign(body, "secret")
	if err := webhook.Verify(body, "secret", sig); err != nil {
		t.Fatal(err)
	}
	if err := webhook.Verify([]byte("tampered"), "secret", sig); err == nil {
		t.Fatal("tampered payload accepted")
	}
	if err := webhook.Verify(body, "wrong", sig); err == nil {
		t.Fatal("wrong secret accepted")
	}
}
