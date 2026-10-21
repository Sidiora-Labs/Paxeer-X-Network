# Next.js Production Readiness Checklist — 100,000+ Users

A complete, domain-by-domain checklist for taking a Next.js (App Router) frontend from "works on my machine" to "survives six-figure concurrent users." Items marked **[RT]** are extra-critical for real-time / financial / trading UIs.

---

## 1. Rendering & Architecture

- [ ] **Deliberate rendering strategy per route** — know exactly which routes are static (SSG), incrementally regenerated (ISR), server-rendered per request (SSR/dynamic), or client-rendered. Don't let routes go dynamic by accident.
- [ ] **React Server Components by default** — keep `"use client"` at the leaves, not the root. Every client component ships JS; minimize the client bundle.
- [ ] **Streaming with Suspense** — stream above-the-fold content first; defer slow data with `<Suspense>` boundaries so TTFB isn't gated by your slowest query.
- [ ] **Partial Prerendering (PPR)** where supported — static shell + dynamic holes, served from the edge.
- [ ] **Runtime chosen per route** — Edge runtime for low-latency/geo-distributed reads; Node runtime where you need full Node APIs or heavier compute.
- [ ] **Route segment config explicit** — `dynamic`, `revalidate`, `runtime`, `fetchCache` set intentionally, not defaulted.
- [ ] **`loading.tsx`, `error.tsx`, `not-found.tsx`** present for every meaningful segment.
- [ ] **Server Actions** used judiciously — understand their POST-per-action cost and rate-limit them.
- [ ] **No giant client trees** — audit with `@next/bundle-analyzer`; split by route and interaction.

## 2. Performance & Core Web Vitals

- [ ] **Budgets enforced in CI** — LCP < 2.5s, INP < 200ms, CLS < 0.1, TTFB < 800ms. Fail the build if exceeded.
- [ ] **JS bundle budget** — set a hard per-route KB limit; track regressions on every PR.
- [ ] **`next/image`** for all images — sizing, `priority` on LCP image, modern formats (AVIF/WebP), responsive `sizes`.
- [ ] **`next/font`** — self-hosted, zero layout shift, subset to needed glyphs (you have AlteixSans / Inter / JetBrains Mono — subset each).
- [ ] **Dynamic imports** (`next/dynamic`) for heavy, below-fold, or rarely-used components (charts, modals, editors).
- [ ] **List virtualization** **[RT]** — `@tanstack/react-virtual` or similar for orderbooks, trade feeds, positions tables. Never render thousands of DOM rows.
- [ ] **Web Workers** **[RT]** — move decoding, heavy math, CBOR/`.kvx` parsing, and aggregation off the main thread.
- [ ] **Avoid main-thread jank** **[RT]** — batch high-frequency WS updates with `requestAnimationFrame`; don't `setState` on every tick.
- [ ] **Memoization where it pays** — `React.memo`, `useMemo`, stable callbacks for hot components; profile before sprinkling.
- [ ] **Prefetching** — `<Link prefetch>` for likely navigations; avoid over-prefetching on data-heavy pages.
- [ ] **Third-party scripts** via `next/script` with correct strategy (`lazyOnload`/`afterInteractive`); audit their cost.
- [ ] **Compression** — Brotli/gzip at the edge/CDN.
- [ ] **Tree-shaking verified** — no accidental full-library imports (lodash, date libs, icon packs).

## 3. Caching & Data Fetching

- [ ] **Explicit cache semantics** — in Next 15, `fetch` is **not** cached by default; opt in per call (`cache`, `next.revalidate`, `use cache`). Document the policy.
- [ ] **CDN/edge caching** for static and ISR assets with correct `Cache-Control` and `s-maxage`/`stale-while-revalidate`.
- [ ] **Client data layer** — TanStack Query or SWR for dedup, background refetch, stale-while-revalidate, retry, and cache invalidation.
- [ ] **Request deduplication & batching** — collapse concurrent identical requests.
- [ ] **Shared server cache** (Redis/Upstash) for ISR coordination and rate-limit counters across instances.
- [ ] **Cache invalidation strategy** — `revalidatePath` / `revalidateTag` wired to your data mutations; tags documented.
- [ ] **Stale data tolerances defined** **[RT]** — prices/balances must never be served stale; static content can be. Be explicit per data type.

## 4. State Management

- [ ] **Server state vs client state separated** — server state in TanStack Query; UI state in Zustand/Jotai; URL state in the URL.
- [ ] **No global store for everything** — scope state to avoid app-wide re-renders.
- [ ] **URL as source of truth** for shareable/restorable state (filters, selected market, tab).
- [ ] **Optimistic updates** **[RT]** with rollback on failure (order placement, cancels).
- [ ] **Hydration-safe** — no `window`/`localStorage` access during render; guard against hydration mismatches.

## 5. Real-Time / WebSocket Layer **[RT]**

- [ ] **Connection manager** — single multiplexed connection where possible; subscribe/unsubscribe per channel.
- [ ] **Auto-reconnect with exponential backoff + jitter** and capped retries.
- [ ] **Heartbeat / ping-pong** and dead-connection detection.
- [ ] **Resubscribe on reconnect** — restore all active subscriptions automatically.
- [ ] **Backpressure handling** — drop/coalesce stale ticks; never queue unbounded.
- [ ] **Sequence/gap detection** — detect missed messages, trigger snapshot resync (critical for orderbooks).
- [ ] **Snapshot + delta model** — initial REST/snapshot, then apply incremental diffs.
- [ ] **Page visibility awareness** — throttle or pause updates when the tab is hidden.
- [ ] **Precision-safe number handling** — use decimal/BigInt for prices and balances; never float arithmetic for money.
- [ ] **Server-Sent Events fallback** where WS is blocked by corporate proxies.

## 6. Security

- [ ] **Strict Content-Security-Policy** — nonce-based; no `unsafe-inline`/`unsafe-eval`. (You've fought CSP/WASM before — budget time for the WASM `wasm-unsafe-eval` directive.)
- [ ] **Security headers** — HSTS, `X-Frame-Options`/`frame-ancestors`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`. Set in `next.config` headers or middleware.
- [ ] **Subresource Integrity (SRI)** for any external scripts.
- [ ] **No secrets in client bundle** — only `NEXT_PUBLIC_*` is exposed; audit for leaked keys.
- [ ] **Input validation** server-side with a schema lib (Zod); never trust client validation alone.
- [ ] **XSS prevention** — sanitize any `dangerouslySetInnerHTML`; avoid it.
- [ ] **CSRF protection** for cookie-based mutations / Server Actions.
- [ ] **Rate limiting & abuse protection** at the edge (per-IP, per-user).
- [ ] **DDoS / WAF** at the CDN layer.
- [ ] **Dependency scanning** — `npm audit`, Dependabot/Renovate, Snyk in CI.
- [ ] **Wallet/signing safety** **[RT]** — keys never leave the wallet; clear signing prompts; nonce/replay protection; SIWE for auth; show exactly what's being signed.
- [ ] **Secrets management** — vault/KMS, not `.env` in the repo; rotate regularly.

## 7. Authentication & Authorization

- [ ] **Battle-tested auth** — Auth.js (NextAuth), Clerk, WorkOS, or a hardened custom flow; SIWE for wallet auth.
- [ ] **Secure session handling** — `httpOnly`, `Secure`, `SameSite` cookies; short-lived access + refresh rotation.
- [ ] **Authorization enforced server-side** — middleware + per-request checks; never rely on hiding UI.
- [ ] **RBAC / scopes** defined and tested.
- [ ] **Token refresh** seamless and race-safe (single-flight refresh).
- [ ] **Logout invalidates server session**, not just client state.
- [ ] **Protected routes** via middleware with minimal latency cost.

## 8. Error Handling & Resilience

- [ ] **Error boundaries** at meaningful granularity; one failing widget shouldn't blank the page.
- [ ] **Graceful degradation** — app remains usable if a non-critical service is down.
- [ ] **Retry with backoff** for transient failures; circuit breaker for repeated failures.
- [ ] **Offline / network-loss UX** — clear banners, queued actions where safe.
- [ ] **User-facing error messages** that are actionable, not stack traces.
- [ ] **Fallback UI** for failed data, not infinite spinners.

## 9. Observability & Monitoring

- [ ] **Error tracking** — Sentry (or equivalent) with source maps uploaded, release tracking, user/session context.
- [ ] **Real User Monitoring (RUM)** — Core Web Vitals from the field, not just lab.
- [ ] **`reportWebVitals`** wired to your analytics.
- [ ] **Structured logging** with request/trace IDs; distributed tracing across edge → API.
- [ ] **Uptime + synthetic checks** on critical flows (login, place order, load market).
- [ ] **Alerting** with sane thresholds and on-call routing — error rate, latency p95/p99, WS disconnect rate **[RT]**.
- [ ] **Product analytics** (privacy-respecting) for funnels and feature usage.
- [ ] **Dashboards** — you already run Prometheus/Grafana on the gateway side; surface frontend metrics there too.

## 10. Testing

- [ ] **Unit tests** — Vitest/Jest for logic, formatters, precision math **[RT]**.
- [ ] **Component tests** — Testing Library for behavior, not implementation.
- [ ] **E2E tests** — Playwright for critical paths (auth, order placement, deposits).
- [ ] **Visual regression** — Chromatic/Playwright snapshots for the design system.
- [ ] **Accessibility tests** — axe in CI.
- [ ] **Load / stress testing** — k6/Artillery against SSR endpoints and WS fan-out **[RT]**.
- [ ] **Contract tests** against your APIs to catch breaking changes.
- [ ] **Coverage gates** in CI on critical packages.

## 11. CI/CD & Deployment

- [ ] **Automated pipeline** — lint, typecheck, test, build, bundle-size check on every PR.
- [ ] **Preview deployments** per PR.
- [ ] **Environment separation** — dev / staging / prod with isolated config and data.
- [ ] **Canary or blue-green rollout** with automated rollback on error-rate spike.
- [ ] **Feature flags** (LaunchDarkly/Unleash/self-hosted) to decouple deploy from release.
- [ ] **Build reproducibility** — locked deps, deterministic builds; Turborepo remote cache if monorepo.
- [ ] **Source maps** uploaded to error tracker but not publicly served.
- [ ] **Zero-downtime deploys** — verified, not assumed.

## 12. Infrastructure & Scaling

- [ ] **CDN in front of everything** with global PoPs.
- [ ] **Multi-region** for static/edge; understand origin proximity for SSR.
- [ ] **Auto-scaling** for SSR/serverless with cold-start mitigation (provisioned concurrency or always-warm).
- [ ] **WS fan-out infrastructure** **[RT]** — dedicated, horizontally scalable gateway (you have the Go gateway); the Next app should not hold long-lived sockets in serverless functions.
- [ ] **Connection/resource limits** understood — serverless concurrency caps, function timeouts.
- [ ] **ISR at scale** — shared cache + on-demand revalidation; avoid thundering-herd regeneration.
- [ ] **Cost monitoring** — watch SSR invocations, bandwidth, image optimization, and edge requests; they scale with users.
- [ ] **Self-hosted option vetted** if Vercel cost/limits don't fit — Docker + Node server behind your own CDN/LB.

## 13. Internationalization & Localization

- [ ] **i18n routing** — next-intl or equivalent; locale in URL or cookie. (You already ship ES, ZH-CN, JA, DE.)
- [ ] **Locale-aware formatting** — numbers, currencies, dates via `Intl`; critical for prices **[RT]**.
- [ ] **No hardcoded strings** — 100% extracted; CI check for untranslated keys.
- [ ] **RTL support** if expanding to Arabic/Hebrew.
- [ ] **Translation workflow** — CLI extraction, missing-key detection, translator handoff.

## 14. Accessibility (a11y)

- [ ] **WCAG 2.1 AA** as the baseline target.
- [ ] **Keyboard navigation** for every interactive element, including custom widgets.
- [ ] **Focus management** — visible focus, focus trapping in modals, restore on close.
- [ ] **Semantic HTML + ARIA** where needed; live regions for real-time price/status updates **[RT]**.
- [ ] **Color contrast** meets AA — verify Paxeer Blue (#004CED) combos against text/background.
- [ ] **Screen reader pass** on critical flows.
- [ ] **Reduced-motion** support — respect `prefers-reduced-motion` (mind your defined motion curves).

## 15. Code Quality & Developer Experience

- [ ] **TypeScript `strict` + `noUncheckedIndexedAccess`** — no `any` in hot paths.
- [ ] **ESLint + Prettier** enforced; `eslint-config-next` plus custom rules.
- [ ] **Pre-commit hooks** — Husky + lint-staged.
- [ ] **Conventional commits** + automated changelog/versioning.
- [ ] **Design system / component library** — single source of truth (tie to your brand tokens).
- [ ] **Storybook** for component dev and visual review.
- [ ] **Monorepo tooling** (Turborepo) with task caching if multi-package.
- [ ] **Documented architecture** — keep ADRs (your `.kvx` session log is well-suited to this).

## 16. UX, PWA & Mobile

- [ ] **Responsive** down to mobile; dense data UIs need real mobile layouts, not shrunk desktop **[RT]**.
- [ ] **PWA** — installable, offline shell, service worker (mind cache invalidation on deploy).
- [ ] **Loading skeletons** matching final layout to avoid CLS.
- [ ] **Empty / error / loading states** designed for every data view.
- [ ] **Theme support** — dark/light with no flash of wrong theme (set before hydration).
- [ ] **Toasts/notifications** for async outcomes (fills, errors) **[RT]**.

## 17. Compliance & Legal

- [ ] **Cookie consent** + preference management (GDPR/CCPA).
- [ ] **Privacy policy & Terms** linked and current (you maintain Sidiora's ToU — keep the frontend in sync).
- [ ] **Audit logging** for sensitive user actions where required.
- [ ] **Data retention & deletion** flows if you store PII.
- [ ] **Geo-restrictions / disclaimers** if regulated jurisdictions apply **[RT]**.

## 18. Pre-Launch Hardening

- [ ] **Load test at 2–3× expected peak** including WS fan-out **[RT]**.
- [ ] **Chaos/failure drills** — kill a region, drop the WS gateway, throttle the API; verify graceful behavior.
- [ ] **Runbook** for common incidents (deploy rollback, cache purge, WS storm).
- [ ] **On-call + alerting** validated end to end (alert actually pages someone).
- [ ] **Lighthouse + WebPageTest** from multiple geographies on real devices.
- [ ] **Penetration test / security review** before public launch.
- [ ] **Rollback tested in production**, not just in theory.

---

### Quick prioritization for a real-time trading frontend

If you're sequencing the work, the items that bite hardest at 100k+ for a data-heavy app are, roughly in order: **WS resilience + backpressure**, **list virtualization + main-thread budget**, **explicit caching/stale-data policy**, **client bundle size**, **observability (so you can see what's breaking)**, then **load testing the WS fan-out**. Everything else is necessary but those are the ones that turn into outages or unusable UIs specifically under concurrency and message volume.