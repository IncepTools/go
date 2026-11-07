# Webhooks example

```bash
go run ./webhooks
```

POST a raw JSON body to `/api/v1/webhooks/incoming` with an
`X-Hub-Signature-256` HMAC-SHA256 signature. The secret is
`development-secret`.

```bash
body='{"action":"push"}'
signature=$(printf '%s' "$body" | openssl dgst -sha256 -hmac development-secret | sed 's/^.* //')
curl -X POST localhost:8084/api/v1/webhooks/incoming \
  -H "X-Hub-Signature-256: sha256=$signature" \
  -H 'content-type: application/json' -d "$body"
```

The handler verifies the raw body before decoding it. See
[`docs/webhooks.md`](../../docs/webhooks.md) for replay protection and
durable acceptance guidance.
