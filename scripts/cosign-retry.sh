#!/usr/bin/env bash
# Run cosign, retrying when the failure is the transparency log rather than us.
#
# Keyless signing writes an entry to rekor.sigstore.dev, a public service with
# no availability promise to this project. Its client gives up after four fast
# attempts, and when it does, goreleaser fails the whole release — after the
# binaries have already been built, which is the expensive part. A short
# outage or a rate-limit burst then costs a full rerun.
#
# So: retry with a long backoff, but only for failures that are not ours. A
# wrong flag or a missing OIDC token fails the same way on every attempt, and
# waiting out four of them helps nobody.
#
# Signing is idempotent — each attempt writes its own transparency log entry,
# and the one that succeeds is the one whose signature we publish.
set -uo pipefail

attempts=${COSIGN_RETRY_ATTEMPTS:-4}
delays=(15 45 90)

# Failures owned by sigstore's services or by the network in between.
transient='rekor\.sigstore\.dev|fulcio\.sigstore\.dev|tuf-repo-cdn\.sigstore\.dev|giving up after|timeout|timed out|deadline exceeded|connection reset|connection refused|no such host|EOF|TLS handshake|Service Unavailable|Too Many Requests|Internal Server Error|status code 4[0-9][0-9]|status code 5[0-9][0-9]'

for attempt in $(seq 1 "$attempts"); do
  out=$(cosign "$@" 2>&1)
  status=$?
  printf '%s\n' "$out"

  if [ "$status" -eq 0 ]; then
    exit 0
  fi

  if ! printf '%s' "$out" | grep -Eqi "$transient"; then
    echo "cosign-retry: failure is not transient, not retrying" >&2
    exit "$status"
  fi

  if [ "$attempt" -ge "$attempts" ]; then
    echo "cosign-retry: sigstore still failing after ${attempts} attempts" >&2
    exit "$status"
  fi

  delay=${delays[$((attempt - 1))]:-90}
  echo "cosign-retry: transient sigstore failure (attempt ${attempt}/${attempts}), retrying in ${delay}s" >&2
  sleep "$delay"
done
