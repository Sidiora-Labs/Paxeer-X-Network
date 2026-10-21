# Existing security regression prerequisite map

`manifest.json` enumerates actual existing Go tests, source hashes, working directories and executable commands. Regenerate with `python3 tools/production/bootstrap/security/build_manifest.py`. This inventory does not qualify task 0.1 or certify production security. Preserve command exits and Go JSON test events; skips and zero-case execution must fail qualification.

## Coverage and limits

- `shared/auth`: EIP-191 valid/address-case/invalid inputs and webhook HMAC golden vectors, timestamp-window checks, missing signature, bad scheme, wrong secret and tampered payload. No durable duplicate-delivery assertion: the HMAC window is freshness checking, not replay deduplication.
- `shared/http`: API-key rejection, CORS response, in-memory/Redis throttling, client-IP resolution, 5xx detail masking and scanner wire-response handling. The Redis test starts a real `redis:7-alpine` container. Do not silently omit it from complete qualification.
- Gateway auth: signed-wallet login, single-use nonce, unknown nonce, bad wallet/signature, missing session, JWT mint/verify/expiry. Login tests require Redis through the gateway internal test helper. JWT-only tests do not establish Redis atomicity under simultaneous login.
- Gateway upload/image: MIME allowlist, unsafe SVG, oversize uploads, invalid address, infected-file rejection and upstream failure. Upload tests inject a scanner function and HTTP upstream; infected-file rejection does not prove live malware scanning. Serve tests include invalid object-key rejection. Proxy tests include client-supplied identity-header stripping and unauthenticated socket rejection.
- Stats, PNL and chart webhook handlers: missing/bad authentication and dispatch/invalid payload handling. Their fixture dispatch tests are not evidence of distributed replay deduplication, provider delivery, queue durability or restart recovery.
- Metadata/user handlers: existing service tests exercise signed write workflows with their package fixtures. These broad service cases require inspection of each subtest before treating them as a complete authorization matrix.
- `shared/secrets`: environment provider, cache expiry/error propagation and unsupported cloud-provider fallback. Existing fallback assertions do not prove managed secret-store integration.

## Deferred security qualification gaps

1. **Scanner fails open:** `shared/http/virusscan.go` returns `Clean: true` when unconfigured, unreachable or timed out. `TestScanBufferNotConfigured` and `TestScanBufferUnreachable` explicitly preserve permissive behavior. Existing clean/infected scanner tests use a local protocol stub, not ClamAV. Required future proof: configured real scanner and EICAR fixture, unavailable scanner rejection/quarantine, timeout/malformed-response behavior and prevention of downstream storage on failed scan. No behavior changed here.
2. **Session binding/concurrency:** add independently specified domain, chain, issued-at/expiry binding and concurrent nonce consumption cases before accepting a complete wallet-login security claim. Current nonce tests cover sequential reuse only.
3. **Webhook replay/key rotation:** current signature tests do not establish durable event replay suppression, exact replay-window boundaries/future timestamps or overlapping key rotation and revocation. Handler success is distinct from committed-once effects.
4. **Trust boundaries:** proxy header-stripping unit tests do not establish hosted ingress isolation, direct backend reachability restrictions, trusted-proxy correctness, private socket authorization or cross-service credential rotation.
5. **Upload adversarial content:** current MIME/SVG examples do not establish parser differential resistance, decompression-bomb limits, polyglot rejection or object-store quarantine lifecycle.
6. **Authorization breadth:** no claim of a complete route-by-role/object-owner matrix, cross-wallet denial, admin compromise boundary or CSRF/browser session analysis follows from these tests.
7. **Frontend and infrastructure:** no security-focused frontend/browser suites or dependency/container scanner execution are registered here; source inventories alone cannot prove XSS/CSP, dependency advisories or deployment hardening.
8. **Coverage scope:** contract security regressions belong to the separate contracts owner; these commands concern existing offchain Go sources only. Private fixture/runner and full application compilation remain coordinator responsibilities.

## Integration

Register each manifest check with the central runner after source hashes are reconciled. Retain per-case JSON events and enforce nonzero exit on skips, unavailable fixture, missing dependencies, stale source hashes and zero matching tests. `-run ^TestWebhook` restricts the three webhook groups; the broad full-package groups intentionally include their existing non-security tests. Container-backed groups must run under the centrally coordinated fixture/container budget. All inventory rows begin `not_executed_by_inventory`; independent execution evidence must not be inferred from their presence.
