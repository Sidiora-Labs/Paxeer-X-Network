# PaxPort production architecture catalog

This catalog is the authoritative inventory of the production application
surface, frozen for `task.1.1` and consolidated by `task.1.3` on 2026-07-28. It records current behavior,
ownership, reachability, trust boundaries, and migration obligations. The
production entry point and route catalog below are the baseline that later
rebuild tasks must preserve or migrate explicitly.

## Production entry point

The production artifact is the Next.js standalone application built from
`src/app`.

```text
src/app/layout.tsx
  QueryProvider
    ErrorBoundary
      src/app/page.tsx
        SplashScreen
          CapacitorProvider
            PWAProvider
              EmbeddedWalletProvider
                WalletKindProvider
                  WalletProvider
                    ShellWidget
              PWA prompts and offline/update status
```

`src/widgets/shell/ShellWidget.tsx` is the only production product shell.

The two additional App Router pages are:

| URL | Owner | Purpose |
| --- | --- | --- |
| `/auth/callback` | custody | Complete the Supabase/OAuth return and hand control back to the opener |
| `/privacy` | product feature | Static privacy information |

The native Android/iOS container uses the same remote production origin,
`https://paxportwallet.com`, through `capacitor.config.ts`; it does not currently
ship the Next.js artifact from `webDir`.

## Production route catalog

`src/domains/shell` defines the single discriminated route model, exact query
schema, serializer, custody/lock/account/feature guards, recovery destination,
data-state contract, and draft lifetime for every route. `useAppRoute` is the
only browser-history owner. It parses reloads and popstate, publishes push or
replace state, and consumes validated native deep-link, notification, and
service-worker navigation events.

| Route | Canonical owner | Current custody behavior | Canonical state |
| --- | --- | --- | --- |
| `portfolio` | `widgets/portfolio/PortfolioWidget` | All provisioned modes | Default `/?`; no route draft |
| `send` | `widgets/send/SendWidget` | Self-custody and managed; funded recovers to portfolio | Optional validated token; draft is route-local |
| `receive` | `widgets/receive/ReceiveWidget` | Self-custody and managed; funded recovers to portfolio | Account and chain come from guarded shell context |
| `transactions` | `widgets/transactions/TransactionsWidget` | All provisioned modes | Identity and freshness come from the data layer |
| `swap` | `widgets/swap/SwapWidget` | All provisioned modes; funded authority is server policy | Quote draft is route-local |
| `discover` | `widgets/discover/DiscoverWidget` | All provisioned modes | Product catalog has no route parameters |
| `settings` | `widgets/settings/SettingsWidget` | All provisioned modes | Mode-specific settings are repository owned |
| `contacts` | `widgets/contacts/ContactsWidget` | Self-custody and managed; funded recovers to settings | Contacts repository owns persistence |
| `ramp` | `widgets/ramp/RampWidget` | Self-custody and managed; funded recovers to portfolio | External handoff is validated at navigation |
| `token-detail` | `widgets/tokenDetail/TokenDetailWidget` | All provisioned modes | Exact token identifier and optional bounded symbol |
| `tx-detail` | `widgets/txDetail/TxDetailWidget` | All provisioned modes | Exact transaction hash; observation is session-bounded |
| `dao` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |
| `paxfun` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Exact pool address and optional bounded symbol |
| `wormhole` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |
| `pns` | `widgets/pns/PNSWidget` | Self-custody; other modes recover to discover | Fixed catalog route |
| `sidiora-fun` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |
| `paxscan` | `widgets/dappBrowser/DAppBrowserWidget` through `paxscan-shim.html` | In-app for self-custody; external for managed/funded | Bounded encoded explorer path |
| `browser` | `widgets/dappBrowser/DAppBrowserWidget` | Self-custody; other modes recover to discover | Credential-free HTTPS URL with query and fragment removed |
| `dex` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |
| `colosseum` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |
| `points` | `widgets/dappBrowser/DAppBrowserWidget` | In-app for self-custody; external for managed/funded | Fixed catalog route |

Before a provisioned shell mounts, `ShellWidget` also owns the loading,
custody-choice, managed sign-in/provisioning, funded sign-in/tier-provisioning,
self-custody onboarding/migration, and self-custody lock states.

## Component ownership and reachability

Runtime ownership follows the production import graph from `src/app/page.tsx`.

| Domain | Production owner |
| --- | --- |
| shell and navigation | `src/widgets/shell`, `src/components/nav` |
| custody selection and wallet session | `src/components/onboarding`, `src/components/auth`, `src/providers/WalletKindProvider.tsx`, `src/providers/WalletProvider.tsx` |
| self-custody boundary | `src/lib/wallet/PaxeerWallet.ts` and its public ports/types |
| managed/funded boundary | `src/lib/wallet/embedded` public adapters |
| portfolio and indexed data | `src/widgets/portfolio`, `src/widgets/tokenDetail`, `src/widgets/transactions`, `src/widgets/txDetail`, `src/lib/data`, `src/lib/queries`, `src/lib/portfolio-api` |
| send and swap | `src/widgets/send`, `src/widgets/swap`, `src/lib/swap` |
| dApp and product features | `src/widgets/dappBrowser`, `src/widgets/discover`, `src/widgets/pns`, `src/widgets/ramp`, `src/widgets/contacts`, `src/widgets/receive` |
| web/PWA/native platform | `src/providers/PWAProvider.tsx`, `src/providers/CapacitorProvider.tsx`, `src/lib/capacitor.ts`, `src/lib/native-dapp-browser.ts`, `public/sw.js` |
| server edge | `src/app/api`, `src/server`, `src/lib/rateLimit.ts`, `src/lib/railwayS3.ts` |
| shared UI | `src/components/ui`, `src/components/SplashScreen.tsx`, success and confirmation components |

The following sources were classified as duplicate or non-authoritative during
the freeze and were removed during consolidation:

| Removed source | Classification | Canonical replacement |
| --- | --- | --- |
| `src/components/WalletShell.tsx` | Unreachable duplicate shell | `src/widgets/shell/ShellWidget.tsx` |
| `src/components/pages/*` | Unreachable parallel Page implementations | Corresponding `src/widgets/*` implementation |
| `src/components/nav/WalletHeader.tsx` | Unreachable legacy header | `src/components/nav/UniversalHeader.tsx` |
| `src/lib/swap/bin/**/*` | Compiled generated artifacts inside source | Authoritative TypeScript ABI modules |
| `src/lib/swap/abis.ts` and `src/lib/swap/sdk/abis.ts` | Unused/overlapping ABI aggregates | Generated `src/lib/swap/sdk/abis/index.ts` |

The retained `src/lib/swap/sdk/abis/*.ts` modules are the authoritative reviewed
ABI inputs. `npm run generate:swap-abis` creates their only public index and
`npm run check:swap-abis` detects drift. `src/lib/swap/sdk/addresses.ts` remains
a vendored deployment-address snapshot from `paxeer-spot-integration-kit` until
Task 8.2 imports and pins the authoritative deployment input.

The empty `src/lib/wallet/core/index.ts` and the application-owned public
PaxLabs SDK contract under `src/lib/wallet/embedded/sdk` remain explicit package
boundaries, not parallel production applications. The latter may contain only
public contract types/client behavior and must not copy server policy or
signing internals.
| `public/icons`, `public/splash_screens`, `public/ui_icons`, fonts, logos | Runtime-loaded/generated asset collections | Retain only referenced release assets; record their generator/source and keep them outside code ownership |

No duplicate source may receive feature work. Missing behavior is migrated to the
canonical widget before deletion.

## Custody capability matrix

The persisted application discriminator uses `self-custody`, `embedded`, and
`funded`; the product language calls the latter two managed and funded.

| Capability | Self-custody | Managed (`embedded`) | Funded |
| --- | --- | --- | --- |
| create/import/migrate vault | Public `PaxeerWallet` facade | PaxLabs provisioning only | PaxLabs funded provisioning only |
| lock/unlock/reauthenticate | Public `PaxeerWallet` facade | Supabase session policy | Supabase and funded policy |
| account add/rename/delete/import/export | Supported through public facade | Unsupported | Unsupported |
| receive address | Supported | PaxLabs public wallet | Intentionally hidden/unsupported |
| native/token send | Local public facade | PaxLabs managed signer | Only authoritative funded-policy calls |
| swap signer | Keyless `VaultSigner` | `EmbeddedSigner` | `FundedSigner` plus server whitelist |
| dApp/PNS signing | Currently reachable | Shell redirects or suppresses | Shell redirects or suppresses |
| reset/logout | Clears self-custody product state | PaxLabs sign-out/reset | PaxLabs funded reset/sign-out |

The current `WalletActions` interface exposes unavailable methods as runtime
throwers. Task 1.2 must replace this with discriminated capabilities so an
unsupported operation is absent from the active type and state.

## Provider and lifecycle ownership

| Provider | Owns | Does not own |
| --- | --- | --- |
| `QueryProvider` | TanStack Query client lifetime | Custody identity or authoritative financial values |
| `ErrorBoundary` | Render-failure containment | Domain error conversion |
| `CapacitorProvider` | Native plugin initialization, deep-link/push events, app lifecycle | Wallet unlock authority |
| `PWAProvider` | install, online, update, and notification prompt state | Push ownership or wallet identity |
| `EmbeddedWalletProvider` | Supabase session and PaxLabs managed/funded adapters | Self-custody vault |
| `WalletKindProvider` | Persisted custody choice | Proof that the chosen backend is available |
| `WalletProvider` | Public wallet snapshot and action dispatch | PaxLabs authentication/signing policy or wallet-core internals |
| `ShellWidget` | Guarded product mounting and shared route lifecycle | Wallet or feature-owned draft internals |

## Application storage inventory

Wallet-core storage is a separate trust boundary and is not migrated into the
application registry.

| Record | Medium | Current owner | Lifecycle/migration obligation |
| --- | --- | --- | --- |
| `paxport-wallet-v2` namespaces | IndexedDB `paxport-wallet-v2/vaults` | wallet-core facade | Preserve; never expose through application repositories |
| `paxport-wallet-session-v1` | IndexedDB `paxport-wallet-v2/vaults` | wallet-core session manager | Non-extractable CryptoKey handle plus bounded expiry only; delete on lock, timeout, reset, credential replacement, invalid restore, or cross-context revocation |
| `paxeer_wallet_state`, `paxeer_pin_hash`, `paxeer_active_account`, legacy session/biometric keys | localStorage | wallet-core legacy migration | Read only through the public migration path, then delete on successful migration/reset |
| Supabase auth records | localStorage managed by Supabase | PaxLabs dependency | Do not copy; lifecycle remains authoritative to Supabase/PaxLabs |
| `paxeer:wallet-kind` | localStorage | custody selection | Validate, reconcile with live state, migrate to registry |
| `paxeer_contacts` | localStorage | contacts | Version, bound, validate, and reset by application lifecycle |
| `paxeer_recent_recipients` | localStorage | send | Version, bound, validate, and scope by identity |
| `paxeer_hide_dust`, `paxeer_hidden_tokens` | localStorage | portfolio | Version and scope by custody/account/chain |
| `pax:meta:*` | localStorage | metadata cache | Add quota, source/schema version, expiry, and corruption signal |
| `paxeer:dapp-tabs` | localStorage | dApp browser/settings | Validate exact origins and reset on lock/custody lifecycle |
| `paxeer_currency`, `paxeer_language` | localStorage | settings | Migrate to locale/preferences repository |
| `paxeer_custom_rpc`, `paxeer_dev_mode`, `paxeer_hex_data` | localStorage | settings | Validate, isolate as expert state, clear on required lifecycle |
| `paxeer_notif_prefs` and `paxeer_notif_*` | localStorage | notifications | Version, privacy-classify, and bind to identity |
| `pwa-install-dismissed`, `pwa-notif-dismissed` | localStorage | PWA | Bound timestamp schema and retention |
| `paxeer_whats_new_seen` | localStorage | announcements | Versioned non-sensitive preference |
| `paxeer_pending_send` | sessionStorage | optimistic activity | Replace with a versioned public operation record; reload observes and never resubmits |
| service-worker caches and push subscription | Cache Storage / PushManager | service worker/PWA | Register cache versions and authenticated subscription ownership |
| `.push-data` JSON files | server filesystem | push server | Replace with atomic durable versioned persistence |

No application-controlled record is approved to contain a mnemonic, private
key, unlock secret, signer/session capability, full sensitive approval payload,
push secret, or authentication credential. The separate wallet-core boundary
may persist only its approved non-extractable, expiry-bound session key handle.

## Server-edge catalog

| Route | Methods | Boundary/upstream |
| --- | --- | --- |
| `/api/wallet/[...path]` | GET, HEAD; POST rejected | Blockscout-compatible indexed wallet API from configured fixed origin |
| `/api/sdk/[...path]` | GET | Sidiora SDK fixed origin |
| `/api/candle/[...path]` | GET | HyperPax candle API |
| `/api/candle/cv/[...path]` | GET | CrossVerse data API |
| `/api/candle/pax/[...path]` | GET | CrossVerse PAX data API |
| `/api/candle/sid/[...path]` | GET | CrossVerse SID data API |
| `/api/sidiora/logo/[...path]` | GET | Sidiora logo origin |
| `/api/sidiora/metadata` | GET | Sidiora batch metadata |
| `/api/token-icon/[address]` | GET | configured S3-compatible object store |
| `/api/token-metadata` | GET | configured S3-compatible object store |
| `/api/chat` | POST | OpenAI chat-completions API |
| `/api/push/subscribe` | POST, DELETE | push subscription filesystem repository |
| `/api/push/send` | GET, POST | admin push campaign service |
| `/api/push/notify-tx` | POST | admin transaction notification service |
| `/api/push/cron` | GET, POST | admin campaign scheduler |
| `/api/health` | GET | process plus RPC, indexer, S3, push configuration checks |

The server edge owns secret environment values, S3 credentials, VAPID private
key, push admin credential, OpenAI credential, fixed upstream origins, and
public error conversion. Client components must not receive those values.

## Native and browser trust boundaries

The native plugin boundary includes Capacitor App, Browser, PushNotifications,
SplashScreen, StatusBar, BiometricAuth, and the registered native dApp-browser
plugin. Deep-link, push, foreground/background, and native dApp messages cross
from OS-controlled input into application state and therefore require the same
route, origin, permission, and approval parsers as web input.

The browser boundary includes iframe `postMessage`, the isolated streamed
Chromium browser plane, service-worker messages, PushManager, Notification,
clipboard, camera/QR scanning, external navigation, remote images/SVG, and
storage. All values enter as untrusted. The browser plane is reached only
through a same-origin bounded proxy authenticated to an internal-only service;
it receives no wallet keys. Each session owns an ephemeral Chromium context,
an unguessable capability, an active tab, and a navigation generation. Remote
provider requests re-enter the same permission and approval engine as iframe
and native requests before any local signing or submission authority is used.

Generic `BiometricAuth` currently authenticates the device user; it is not a
wallet-core hardware-backed unlock slot and must not be described as wallet
unlock.

## Approved external origins and current owners

| Origin | Purpose | Owner |
| --- | --- | --- |
| `connect.paxportwallet.com` | managed/funded public wallet API | custody adapter |
| configured Supabase project | managed/funded authentication | PaxLabs/Supabase |
| configured Paxeer RPC origins | chain reads and submission | custody/transaction data |
| configured Blockscout origin | portfolio/activity index | server edge |
| `sidiora.fun` | SDK and metadata | server edge/swap |
| `data-api.crossverse.app` | market/candle data | server edge/data |
| `NEXT_PUBLIC_PNS_API_BASE` | PNS reads | PNS adapter |
| `api.openai.com` | informational assistant | server edge |
| configured S3 endpoint | token icons and metadata | server edge |
| `app.hyperpax.xyz`, `colosseum.hyperpaxeer.com`, `dao.hyperpaxeer.com`, `crossverse.app`, `app.webpoints.app`, `paxscan.io`, `kindlelaunch.com` | user-visible external products | dApp/navigation policy |
| reviewed token-logo origins | untrusted display media | media proxy |

Arbitrary HTTPS origins are not approved merely because the current Next image
configuration accepts them. Task 2.1 must reduce the set to reviewed origins or
a bounded same-origin media proxy.

## Dependency direction

The target dependency direction is:

```text
app entry -> shell/providers -> feature widgets -> domain services
          -> custody public facade or managed/funded adapter
          -> platform/server-edge boundary
```

Feature code may not import wallet-core managers, server secrets, raw Supabase
clients, mutable shared caches, or another feature's internal components.
Boundary data enters as `unknown` and is parsed before it becomes a domain type.

## Remaining migration work

- Unsupported custody actions remain callable and fail through routine runtime
  errors.
- Remote native authority, arbitrary image hosts, scattered external
  navigation, inline service-worker registration, and copied/generated sources
  remain active perimeter obligations.

Removal or redesign must preserve user data and shipped behavior intentionally;
none of these defects may be hidden by deleting the evidence before its
replacement is verified.
