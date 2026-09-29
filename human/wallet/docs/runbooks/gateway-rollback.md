# Gateway rollback

The wallet gateway runs as the application `paxeer-wallet-gateway` from
`human/wallet/deploy/gateway.toml`, built from `docker/wallet-gateway/Dockerfile`
at the repository root and deployed with `--image` (observation 3.7.4). It
applies the SQL files under `human/wallet/gateway/migrations` in filename
order at start and never reverts one. A rollback therefore returns the
gateway's code to an earlier image; it never returns the database schema.

## Trigger

- After a deployment, `curl -sS https://<gateway-base>/readyz` answers 503
  for a component that was `up` before it, or `/healthz` stops answering.
- `tools/wallet/check-live.sh gateway` (added by task 4.1) fails where it
  passed on the previous release.
- Signing, provisioning or broadcast errors rise after a deployment with the
  attestor network, the identity provider and the RPC pool healthy.

## Preconditions

- The fault follows a gateway deployment. A fault in the attestors, the
  identity provider or the RPC pool is handled by its own runbook; rolling the
  gateway back does not repair it.
- The rollback target image contains every migration file already applied to
  the database, so its code runs on the current schema. Once any wallet
  carries `migrated_at`, the target must contain
  `migrations/006_attestor_signing.sql` and the attestor signing path in
  `src/routes/sign.ts`; an older image cannot sign for a migrated wallet.
- After the endpoint cutover, the proxy on the current host keeps pointing at
  `paxeer-wallet-gateway`; a gateway rollback does not touch the proxy.

## Commands

1. List the releases with their images.

   ```sh
   flyctl releases --app paxeer-wallet-gateway --image
   ```

   Expected: releases newest first, each with version, status and image
   reference. The rollback target `<previous-image>` is the image of the
   newest release before the faulty one whose status is `complete`.

2. Check that the target carries every applied migration.

   ```sh
   psql "$DATABASE_URL" -Atc "select filename from _migrations order by filename"
   git -C <repository-checkout> ls-tree --name-only <target-revision> human/wallet/gateway/migrations/
   ```

   Expected: every name in the first list appears in the second.

3. Deploy the target image.

   ```sh
   flyctl deploy --config human/wallet/deploy/gateway.toml --image <previous-image> --app paxeer-wallet-gateway --ha=false -y
   ```

   Expected: every machine replaced with the target image and passing the
   platform check.

4. Watch the log of the rolled-back machines.

   ```sh
   flyctl logs --app paxeer-wallet-gateway --no-tail
   ```

   Expected: migrations report nothing to apply, the server listens on `PORT`,
   and no start-up error.

## Readiness check that proves recovery

```sh
curl -sS https://<gateway-base>/healthz
curl -sS https://<gateway-base>/readyz
```

`/healthz` answers `ok: true`; `/readyz` answers 200 with `"ready":true`
and `attestors`, `nonce_store`, `rpc_pool` and `identity_provider` all `up`;
`tools/wallet/check-live.sh gateway` passes. The platform check in
`gateway.toml` requests `/health/ready`, a path the gateway does not serve
(observation 4.5.1); readiness is read from `/readyz`. Record revision,
command, exit code and log path in `spec/paxeer-x-wallet/qualification.kvx`.

## Rollback

Redeploy the faulty release's image, read from step 1, with the command of
step 3. Nonces allocated by either release sit in the shared nonce store
(`nonce_allocations`), so neither direction reuses a nonce.
