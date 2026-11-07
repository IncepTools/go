# Webhooks

`web/webhook` provides the small, security-critical part of a webhook
receiver: signing and verifying the **raw request bytes** with HMAC-SHA256.
The package does not decide which provider headers, replay window or event
schema your application uses. Those are provider and domain concerns and
belong in the module's service.

## The request boundary

Verify before decoding JSON. JSON can be semantically equivalent while its
bytes differ, and providers sign the bytes they sent.

```go
func (c *Controller) receive(ctx fiber.Ctx) error {
	body := append([]byte(nil), ctx.Body()...)
	if err := webhook.Verify(
		body,
		cfg.WebhookSecret,
		ctx.Get("X-Hub-Signature-256"),
	); err != nil {
		return fiber.ErrUnauthorized
	}

	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		return errs.Invalid("invalid webhook JSON")
	}
	return svc.Receive(ctx.Context(), event)
}
```

Keep this handler thin. Signature verification is transport work; deciding
what an event means belongs in the service.

## Signing and verification

```go
signature := webhook.Sign(payload, secret)
if err := webhook.Verify(payload, secret, signature); err != nil {
	// reject
}
```

`Sign` returns the conventional `sha256=<hex>` form. `Verify` rejects missing,
malformed, differently-prefixed and incorrect signatures and compares the
decoded MAC with `hmac.Equal`, which avoids an early-exit comparison leak.

Never log the secret or the complete authorization header. It is fine to log
the provider event id, delivery id and verification result.

## Replay protection

HMAC proves authenticity, not freshness. If a provider supplies a timestamp,
verify it before accepting the event and reject deliveries outside a bounded
window. If it supplies a delivery id, store it with a unique constraint and
make a repeated delivery a successful no-op.

```go
if time.Since(event.Timestamp) > 5*time.Minute {
	return errs.Invalid("stale webhook")
}
if err := deliveries.InsertOnce(ctx, event.DeliveryID); err != nil {
	return err
}
```

The exact timestamp format and signed header vary by provider, so the core
package intentionally does not guess them.

## Acknowledge after durable work

Providers retry non-2xx responses. If accepting an event must survive a
restart, write the delivery and its outbox message in one transaction, then
return 2xx. Do not return success after only updating in-memory state.

See [Transactional outbox](outbox.md) and [Idempotency](idempotency.md).

## Routing and guards

Webhook endpoints are often authenticated by their signature rather than a
user token. Mark the route public in the route registry, but keep signature
verification mandatory in the handler or service:

```go
router.Post("/", c.receive).
	AsPublic().
	Summary("Receive a signed provider delivery")
```

`AsPublic` means “no user guard”; it must never mean “trust the payload”.

## Testing

Test the cryptographic boundary with the exact bytes sent by the provider:

```go
body := []byte(`{"action":"push"}`)
sig := webhook.Sign(body, "test-secret")

if err := webhook.Verify(body, "test-secret", sig); err != nil {
	t.Fatal(err)
}
if err := webhook.Verify([]byte(`{"action":"pull_request"}`), "test-secret", sig); err == nil {
	t.Fatal("changed payload was accepted")
}
```

Also test malformed signatures, wrong secrets, stale timestamps and duplicate
delivery ids in the module service tests.

## Example

Run the dedicated example:

```bash
cd examples
go run ./webhooks
```

The endpoint is `POST /api/v1/webhooks/incoming` and uses
`development-secret`.

## Next

- [Transactional outbox](outbox.md) — durable acceptance and delivery.
- [Idempotency](idempotency.md) — duplicate delivery protection.
- [Security](security.md) — guards and transport boundaries.
