# Agent integration guide

**Audience:** coding agents and automation integrating `@paxeer-network/i18n`.

This file is a **routing layer**: invariants, scenario → approach, and anti-patterns. For full props, effects, and copy-paste recipes, follow the links under [Further reading](#further-reading).

### Required companion docs (implementation)

When this guide is used to **implement**, **migrate**, or **modify** localization (e.g. wire `LocaleProvider`, `AsyncResources`, HTTP `backend`, extend keys, or language switching), **always** bring **both** companion files into context **together with** this one — **do not** implement from `AGENTS.md` alone.

| File                               | Purpose                                                                      |
| ---------------------------------- | ---------------------------------------------------------------------------- |
| [integration.md](./integration.md) | Props, effects, loading strategies, `registerResources`, external resources  |
| [examples.md](./examples.md)       | End-to-end recipes (Vite, Next/webpack, HTTP `public/`, sync maps, URL sync) |

**For coding agents:** Before writing or editing integration code, **read** `integration.md` and `examples.md` (same directory as this file). In Cursor-style UIs, attach **`@integration.md`** and **`@examples.md`** alongside **`@AGENTS.md`**, or attach the whole **`docs/guide`** folder if supported.

## Default bundled integration (preferred)

When the app **bundles** paxeer-network SDK locale JSON together with app `extend` JSON (the common case), **default to** `LocaleProvider` with **`resources` typed as `AsyncResources`**. For **new** integrations, do **not** default to HTTP `backend` or a fully static `Resources` map unless product or ops explicitly require runtime-only JSON (CDN / `public/` without bundling) or a sync map.

**Existing code uses HTTP `backend`:** If the target project already has `backend={{ loadPath }}` (or equivalent) and this guide is in scope, treat the **intent as refactoring** to **`AsyncResources`**: drop the HTTP loader for those same locale bundles, implement the async loader per the bundler row below (merge SDK + extend per [Examples](./examples.md)), and keep **one** primary loading path ([Integration — loading strategies](./integration.md#loading-strategies-quick-reference)). Only **keep** `backend` when the user explicitly asks to stay on HTTP-only loading.

The **same** `AsyncResources` API applies everywhere; only the **loader implementation** depends on the bundler:

| Bundler                  | How to load JSON inside `AsyncResources`                                                          | Recipe                                                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| **Vite** (or compatible) | **`import.meta.glob`** with **string-literal** patterns only                                      | [Async resources (Vite)](./examples.md#async-resources-vite)                              |
| **Next.js / webpack**    | **Explicit `import()`** per locale (or equivalent static analysis) — **never** `import.meta.glob` | [Async resources (Next.js and webpack)](./examples.md#async-resources-nextjs-and-webpack) |

**Agent workflow:** Infer the toolchain from `vite.config.*`, `next.config.*`, or `package.json`, then follow the matching row. If unclear, ask or inspect the build tool before generating loader code.

## Package responsibility

`@paxeer-network/i18n` provides internationalization for paxeer-network apps: locale types, constants, React context and providers, a shared **i18next** instance, optional HTTP `Backend` loading, and path/locale helpers. It does **not** ship UI components; consumers use types, hooks, and providers.

## Package surface (quick reference)

| Import                            | Role                                                                                                                                                                                                                                                                                                               |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `@paxeer-network/i18n`           | Main entry: `LocaleProvider`, `ExternalLocaleProvider`, `LanguageProvider`, `registerDefaultResource`, `registerResources`, `Backend`, hooks (`useTranslation`, `useLocaleCode`, `useRegisterExternalResources`), `i18n`, types, baseline `en` messages, re-exports from `react-i18next` / `i18next` as applicable |
| `@paxeer-network/i18n/locales/*` | Built-in locale JSON                                                                                                                                                                                                                                                                                               |
| `@paxeer-network/i18n/constant`  | e.g. `LocaleEnum`, `defaultLanguages`, `defaultNS`                                                                                                                                                                                                                                                                 |
| `@paxeer-network/i18n/utils`     | Path and locale helpers — see [Utils](./utils.md)                                                                                                                                                                                                                                                                  |

**Default i18n namespace:** `translation` (`defaultNS`). Details: [exports](./exports.md).

## Invariants (do not violate)

1. **Singleton `i18n`:** `LocaleProvider` composes `LanguageProvider` and `I18nextProvider` around the package’s **single** `i18n` instance. Integrations should not create a parallel i18next stack for the same tree unless you deliberately use `createI18nInstance` / separate trees (advanced; not the default path).

2. **`AsyncResources` and `ns`:** Type is `(lang, ns) => Promise<Record<string, string>>`. When loading goes through **`registerResources`** (from `LocaleProvider`, `ExternalLocaleProvider`, or `useRegisterExternalResources`), the implementation calls `resources(localeCode, defaultNS)` — the second argument is **always** `defaultNS` (`translation`), not an arbitrary namespace. Use the `ns` parameter only if you build URLs or need it for documentation; for multiple i18next namespaces, use the i18n API directly.

3. **One primary loading strategy per app:** Prefer exactly one of — HTTP `backend` · static or async `resources` · controlled `locale` + `resource` — to avoid overlapping bundles and double registration. See [Integration §1](./integration.md#1-wrap-your-app-with-localeprovider).

4. **Custom keys:** Use the `extend.` prefix for app-specific keys so they stay distinct from SDK keys and align with CLI tooling (e.g. `separateJson`). See [Integration §3](./integration.md#3-extending-locale-files).

5. **`useRegisterExternalResources`:** Pass a **stable** `resources` reference (`useCallback` for async loaders; module scope or `useMemo` for static maps) to avoid unnecessary re-registration.

## Decision table (goal → approach → doc)

| Goal                                                                   | Approach                                                                                                                                                                     | Read                                                                               |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Vite (or compatible bundler) — merge SDK base JSON + app `extend` JSON | `LocaleProvider` + `resources` as **`AsyncResources`**, often with `import.meta.glob` (literal patterns only)                                                                | [Examples — Async resources (Vite)](./examples.md#async-resources-vite)            |
| Next.js / webpack — same merge idea                                    | Explicit per-locale `import()`; **do not** rely on `import.meta.glob`                                                                                                        | [Examples — Next.js and webpack](./examples.md#async-resources-nextjs-and-webpack) |
| Serve JSON from `public/` (or CDN) via URLs only (no bundled merge)    | `backend={{ loadPath }}` on `LocaleProvider` — **`public/` / CDN files are maintained by the host app** (manual copy or script / Husky); the package does **not** write them | [Examples — HTTP backend](./examples.md#http-backend)                              |
| All locales in a static in-memory map                                  | `resources` as static `Resources`                                                                                                                                            | [Examples — Sync resources](./examples.md#sync-resources)                          |
| Narrow language picker to a subset                                     | `supportedLanguages` / `languages` via `LanguageProvider` props                                                                                                              | [Examples — Custom languages](./examples.md#custom-languages)                      |
| Sync locale with URL (e.g. `/en/...`)                                  | `onLanguageChanged` (and related routing)                                                                                                                                    | [Examples — URL sync](./examples.md#onlanguagechanged-url-sync)                    |
| Inject one flat bundle for one language (controlled)                   | `locale` + `resource` (only when `resources` is unset)                                                                                                                       | [Integration §1](./integration.md#1-wrap-your-app-with-localeprovider)             |
| Host / external bundles without extra wrapper                          | `ExternalLocaleProvider`, or `useRegisterExternalResources` under `LocaleProvider` — same **`registerResources`** path as `LocaleProvider.resources`                         | [Integration §4](./integration.md#4-integrate-external-resources)                  |
| Merge English keys before first paint (reduce raw-key flicker)         | `registerDefaultResource` at bootstrap (before React mount)                                                                                                                  | [Integration §4](./integration.md#registerdefaultresource)                         |

## Do not

- Use **`import.meta.glob`** on **Next.js / webpack** for locale loading — use explicit `import()` patterns from [Examples](./examples.md#async-resources-nextjs-and-webpack).
- Pass **non-literal** strings to `import.meta.glob` — bundlers must see literal glob patterns at build time ([Examples — Vite](./examples.md#async-resources-vite)).
- Combine **HTTP `backend`** with heavy overlapping **`resources`** for the same namespaces without understanding merge order — prefer one primary strategy ([Integration](./integration.md#loading-strategies-quick-reference)). When migrating **from** `backend` **to** `AsyncResources`, remove `backend` for the replaced bundles instead of stacking both.
- Assume **`loadPath` URLs work without placing files** — copy or sync SDK `dist/locales` (and extend JSON) to `public/` or CDN **yourself**, or automate with a script / Husky ([Examples — HTTP backend](./examples.md#http-backend)).
- Assume **`AsyncResources`** receives arbitrary `ns` from `registerResources` — second argument is always **`translation`** when going through that path ([Integration](./integration.md#locale-only-props)).
- Omit a **stable** reference for `useRegisterExternalResources` — causes redundant work ([Integration §4](./integration.md#useregisterexternalresources)).

## Minimal snippet

```tsx
import { LocaleProvider } from "@paxeer-network/i18n";

export function App() {
  return (
    <LocaleProvider>
      <YourApp />
    </LocaleProvider>
  );
}
```

English is available by default; multiple locales, bundling, HTTP loading, and CLI workflows are in [Examples](./examples.md) and [Integration](./integration.md).

## Further reading (order)

1. [Integration](./integration.md) — props, effects, loading strategies, external resources.
2. [Examples](./examples.md) — end-to-end recipes (Vite, Next/webpack, HTTP, sync, URL sync).
3. [Exports](./exports.md) — entry points and hooks summary.
4. [Utils](./utils.md) — `parseI18nLang`, `generatePath`, etc.
5. [CLI](./cli.md) — CSV/JSON tooling for translation workflows.

## Search keywords

`@paxeer-network/i18n`, `LocaleProvider`, `ExternalLocaleProvider`, `LanguageProvider`, `I18nProvider`, `useTranslation`, `useLocaleCode`, `useRegisterExternalResources`, `useLanguageContext`, `AsyncResources`, `bundled`, `migrate`, `refactor`, `Resources`, `registerResources`, `registerDefaultResource`, `Backend`, `loadPath`, `public`, `Husky`, `copyLocales`, `defaultNS`, `translation`, `i18next`, `react-i18next`, `LocaleCode`, `extend.`, `import.meta.glob`, `import()`, `importLocaleJsonModule`, `Vite`, `Next.js`, `webpack`, `integration.md`, `examples.md`, `docs/guide`
