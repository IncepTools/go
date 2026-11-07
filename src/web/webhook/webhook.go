// Package webhook contains transport-independent helpers for signed webhooks.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrInvalidSignature indicates that a webhook signature is missing or invalid.
var ErrInvalidSignature = errors.New("webhook: invalid signature")

// Sign returns the conventional "sha256=<hex>" signature.
func Sign(payload []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write(payload)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// Verify validates a signature using constant-time comparison.
func Verify(payload []byte, secret, signature string) error {
	signature = strings.TrimSpace(signature)
	if !strings.HasPrefix(signature, "sha256=") {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return ErrInvalidSignature
	}
	expected := hmac.New(sha256.New, []byte(secret))
	_, _ = expected.Write(payload)
	if !hmac.Equal(provided, expected.Sum(nil)) {
		return ErrInvalidSignature
	}
	return nil
}
