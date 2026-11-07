# webhooks

Verified inbound deliveries, with replay detection.

```bash
go run ./webhooks

BODY='{"id":"d-1","action":"created"}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac development-secret -r | cut -d' ' -f1)

curl -X POST localhost:8084/api/v1/webhooks/incoming \
  -H "X-Hub-Signature-256: sha256=$SIG" -d "$BODY"   # 202, duplicate=false
curl -X POST localhost:8084/api/v1/webhooks/incoming \
  -H "X-Hub-Signature-256: sha256=$SIG" -d "$BODY"   # 202, duplicate=true
curl -X POST localhost:8084/api/v1/webhooks/incoming \
  -H 'X-Hub-Signature-256: sha256=deadbeef' -d "$BODY"   # 401
```

## Layout

```
apps/webhooks/                  the domain
  webhooks_app.go               Descriptor, Providers, NewApp
  incoming/                     the module
    incoming_module.go          Providers, New, subscriptions
    constants/                  module name, header name, messages
    dto/                        Delivery, Receipt
    service/                    verification, replay detection, dispatch
    controller/                 the HTTP surface
```

`main.go` builds the application and mounts the domain. It does not know the
webhook secret, which bus the subscribers use, or that there is a replay set:
each of those is owned by the module that needs it.

## Three things this example is really about

**Verify before you parse.** The signature covers the exact bytes the sender
sent. Decoding first and re-encoding to check compares against bytes nobody
signed — key order and whitespace both change — so the raw body goes to
`webhook.Verify` and only then to `json.Unmarshal`.

**Redelivery is normal, not an error.** A sender that does not get a 2xx
retries, so the same delivery arrives more than once as a matter of course.
The service records delivery ids and reports a duplicate as a successful 202.
Treating it as a failure is how a webhook endpoint double-charges a customer.
The set is in memory here; a real one needs to survive a restart and be shared
across replicas, so it belongs in the database or in Redis with a TTL.

**202, not 200.** The subscribers run after the response. Reporting that the
work is done, when it has only been accepted, tells the sender to stop
retrying something that may still fail.

See [`docs/webhooks.md`](../../docs/webhooks.md) for durable acceptance.
