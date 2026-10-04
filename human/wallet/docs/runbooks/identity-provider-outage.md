# Identity provider outage

Supabase Auth issues every user's access token. The gateway verifies tokens
against the project's JWKS at `${SUPABASE_URL}/auth/v1/.well-known/jwks.json`
(`human/wallet/gateway/src/auth/jwt.ts`, ten-minute key cache, five-second
fetch timeout), and every attestor verifies the same token itself against
`ATTESTOR_JWKS_URL` with `ATTESTOR_JWT_ISSUER` and `ATTESTOR_JWT_AUDIENCE`
(`human/wallet/attestor/internal/auth/jwt`), keeping the fetched key set in
memory and refetching only on an unknown key id. Both fail closed: with no
key that verifies a token, nothing is signed. There is no fallback identity
path and none is added during an outage.

## Trigger

- The gateway readiness route reports the identity provider down:
  `curl -sS https://<gateway-base>/readyz` answers 503 with
  `components.identity_provider.state` `down` and a reason such as
  `jwks answered 503`, `jwks_empty` or a fetch timeout.
- Sign requests fail in the token category (`token_invalid`,
  `token_unavailable`) across every attestor while tokens are fresh.
- Users cannot complete sign-in.

## Preconditions

- The attestor network is healthy: `scripts/wallet/check-live.sh attestors`
  passes. An identity outage and an attestor outage are handled separately.
- The gateway's other readiness components (`attestors`, `nonce_store`,
  `rpc_pool`) are `up`, so the outage is isolated to identity.

## Commands

1. Confirm the provider itself is failing, from outside the platform.

   ```sh
   curl -sS -o /dev/null -w '%{http_code}\n' "$SUPABASE_URL/auth/v1/.well-known/jwks.json"
   ```

   Expected during the outage: a 5xx code or a timeout. A 200 here while
   readiness reports `down` points at the gateway's egress; check
   `flyctl logs --app paxeer-wallet-gateway --no-tail` for fetch errors.

2. Read the gateway readiness report.

   ```sh
   curl -sS https://<gateway-base>/readyz
   ```

   Expected during the outage: `"ready":false` with the identity provider
   `down`, the other components `up`.

3. Keep every gateway and attestor machine running. Do not restart,
   redeploy or scale the gateway or any attestor during the outage: a restart
   empties the in-memory key sets, and a process that starts during the
   outage cannot verify any token at all.

   ```sh
   flyctl status --app paxeer-wallet-gateway
   ```

   Expected: every machine `started`.

4. Watch the provider until its JWKS answers 200 with keys again.

   ```sh
   curl -sS "$SUPABASE_URL/auth/v1/.well-known/jwks.json" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["keys"]))'
   ```

   Expected on recovery: a key count of one or more.

## Readiness check that proves recovery

```sh
curl -sS https://<gateway-base>/readyz
```

The route answers 200 with `"ready":true` and
`components.identity_provider` `up` with `keys` of one or more. A fresh
sign-in and a signature through the gateway for a test identity pass
`scripts/wallet/check-live.sh gateway` (added by task 4.1). If the provider
rotated its signing key during the outage, the attestors fetch the new key set
on the first token with the new key id; no action is needed. Record revision,
command, exit code and log path in `spec/paxeer-x-wallet/qualification.kvx`.

## Rollback

Nothing is changed by this runbook, so nothing is rolled back. If a machine
was restarted during the outage and cannot verify tokens once the provider
has recovered, restart it again with
`flyctl machine restart <machine-id> --app <application>` so it fetches the
key set afresh.
