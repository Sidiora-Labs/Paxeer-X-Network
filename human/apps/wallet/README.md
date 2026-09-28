# Paxeer X wallet app

The Next.js and Capacitor wallet app of Paxeer X Network.

## Scripts

- `pnpm install --frozen-lockfile` installs dependencies from `pnpm-lock.yaml`.
- `pnpm run type-check` runs the TypeScript check.
- `pnpm exec vitest run` runs the unit suite.
- `scripts/scan-secrets.sh [dir]` scans the app tree, or `dir`, for committed secrets and local artefacts; it prints `path:line: class` for every hit without the matched value and exits 1 on any hit, 0 when clean. `scripts/scan-secrets.test.sh` exercises it against generated fixtures.

## Secrets and configuration

The app was brought under version control with every secret and local artefact removed. Nothing below may be committed again; each value is supplied through the environment at build or run time.

### Removed

| Removed | Why | Replacement |
| --- | --- | --- |
| A standalone contract test script holding a real private key | A private key in source control is compromised for good | Deleted; no code path needed it |
| `.env` and `.env.production` | Environment files carry deployment credentials and backend addresses | The variables listed below, set on the platform |
| The production image step that copied `.env.production` into the runtime image | The image baked configuration and a credential into a layer | Environment variables set on the container at build and run time |
| A Supabase publishable key and project address in the embedded wallet guide | A credential and an internal project address | `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`, `NEXT_PUBLIC_SUPABASE_URL` |
| The name service indexer address in the name service client, the content security policy and the architecture notes | Internal backend host | `NEXT_PUBLIC_PNS_API_BASE` |
| The points indexer address in the points client | Internal backend host | `NEXT_PUBLIC_POINTS_API_BASE` |
| The storage project origin in the content security policy image list | Internal project address | `NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN` |
| The explorer backend address | Internal backend host | `BLOCKSCOUT_UPSTREAM_BASE` |
| An absolute working directory of a host in the process manager file | Names a host layout | The process manager now runs from the file's own directory |
| Literal recovery phrases in the vault, session and wallet tests and the browser storage harness | Recovery phrases in source look like leaked secrets and trip the scanner | Each test derives its phrase from fixed entropy with `@scure/bip39` at run time, so known-answer assertions are unchanged |

### Environment variables

| Variable | Scope | Purpose |
| --- | --- | --- |
| `NEXT_PUBLIC_SUPABASE_URL` | build | Supabase project URL for sign-in |
| `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` | build | Supabase publishable key; embedded sign-in is disabled when unset |
| `NEXT_PUBLIC_PAXEER_WALLET_API` | build | Embedded wallet API base |
| `NEXT_PUBLIC_PAXEER_RPC_URL` | build | Chain RPC base |
| `NEXT_PUBLIC_PNS_API_BASE` | build | Name service indexer base; name lookups return nothing when unset, and its origin joins the policy's connect list when set |
| `NEXT_PUBLIC_POINTS_API_BASE` | build | Points indexer base; the points balance is absent when unset |
| `NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN` | build | Storage origin allowed as an image source by the content security policy |
| `BLOCKSCOUT_UPSTREAM_BASE` | run | Explorer backend behind the same-origin wallet data route |
| `OPENAI_API_KEY` | run | Assistant route credential |
| `VAPID_PRIVATE_KEY`, `NEXT_PUBLIC_VAPID_PUBLIC_KEY`, `VAPID_SUBJECT` | run, build | Web push keys; generate with `scripts/generate-vapid-keys.js` |
| `PUSH_ADMIN_KEY` | run | Push administration routes |
| `TRUSTED_PROXY_SECRET` | run | Shared secret between the edge proxy and the app |
| `BROWSER_PLANE_INTERNAL_KEY` or `BROWSER_PLANE_INTERNAL_KEY_FILE` | run | Browser plane credential, inline or by file path |
| `SENTRY_DSN`, `NEXT_PUBLIC_SENTRY_DSN` | run, build | Error reporting; reporting is off when unset |

`.env` and `.env.*` are ignored by git, together with build output, native intermediates, signing files and editor and OS artefacts; see `.gitignore`.
