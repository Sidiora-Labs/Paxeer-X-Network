# Examples

**Prerequisite:** Read [Integration guide](./integration.md) first—especially [§1 Wrap Your App with LocaleProvider](./integration.md#1-wrap-your-app-with-localeprovider) (props and behavior) and [§4 Integrate external resources](./integration.md#4-integrate-external-resources) (`registerResources` and external bundles)—so these recipes align with how the package loads data.

Pick a section by goal:

| Goal                                                            | Section                                                                      |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Vite + `import.meta.glob`, merge SDK base + extend JSON         | [Async resources (Vite)](#async-resources-vite)                              |
| Next.js (webpack) + explicit per-locale `import()` for SDK JSON | [Async resources (Next.js and webpack)](#async-resources-nextjs-and-webpack) |
| JSON under `public/`, load via HTTP `backend.loadPath`          | [HTTP backend](#http-backend)                                                |
| Static `Resources` map (sync imports)                           | [Sync resources](#sync-resources)                                            |
| Subset of locales in the picker                                 | [Custom languages](#custom-languages)                                        |
| Locale in the URL (e.g. `/en/...`) when switching language      | [onLanguageChanged (URL sync)](#onlanguagechanged-url-sync)                  |

**Recommendations:** Prefer **`AsyncResources`** with Vite **`import.meta.glob`** or Next.js **explicit base imports**; use **`backend.loadPath`** when serving JSON from `public/`. On Next/webpack, do **not** rely on `import.meta.glob`—see [Async resources (Next.js and webpack)](#async-resources-nextjs-and-webpack).

## Table of Contents

- **Locale loading**
  - [Async resources (Vite)](#async-resources-vite)
  - [Async resources (Next.js and webpack)](#async-resources-nextjs-and-webpack)
  - [HTTP backend](#http-backend)
  - [Sync resources](#sync-resources)
- [Custom languages](#custom-languages)
- [onLanguageChanged (URL sync)](#onlanguagechanged-url-sync)

## Async resources (Vite)

_Bundled SDK + extend JSON. Pass an `AsyncResources` loader to `LocaleProvider`._

Use this pattern when you use **Vite** (or a compatible bundler with `import.meta.glob`). Glob patterns **must be string literals** so the bundler can analyze them at build time—do not build paths from variables or template concatenation.

- **Base SDK strings:** glob `node_modules/@paxeer-network/i18n/dist/locales/*.json` (path may differ slightly depending on your install layout).
- **Extend keys:** glob your app’s locale files (example: `./locales/*.json`—adjust the relative path to match your project).
- For **`en`**, return your imported extend file only: the English base is already registered by the package, so you avoid an extra base fetch.
- For other locales, `Promise.all` base + extend and merge. **`importLocaleJsonModule`** normalizes each module’s `default` export.

**Reference implementation:** WOOFi-dex `src/components/paxeerNetworkProvider/paxeerNetworkLocaleProvider.tsx` (same merge and `en` optimization; glob keys must match your file locations).

```typescript
import { FC, ReactNode } from "react";
import {
  AsyncResources,
  LocaleProvider,
  LocaleCode,
  importLocaleJsonModule,
  type LocaleJsonModule,
  LocaleEnum,
} from "@paxeer-network/i18n";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import extendEnLocale from "./locales/en.json";

// Glob patterns must be static string literals (Vite resolves them at build time).
const baseLoaders = import.meta.glob<LocaleJsonModule>(
  "/node_modules/@paxeer-network/i18n/dist/locales/*.json",
);
// Adjust this glob to where your app stores extend locale JSON (e.g. "../../locales/*.json").
const extendLoaders = import.meta.glob<LocaleJsonModule>("./locales/*.json");

async function loadBase(lang: LocaleCode): Promise<Record<string, string>> {
  const key = `/node_modules/@paxeer-network/i18n/dist/locales/${lang}.json`;
  return importLocaleJsonModule(baseLoaders[key]);
}

async function loadExtend(lang: LocaleCode): Promise<Record<string, string>> {
  const key = `./locales/${lang}.json`;
  return importLocaleJsonModule(extendLoaders[key]);
}

const resources: AsyncResources = async (lang: LocaleCode, _ns: string) => {
  if (lang === LocaleEnum.en) {
    return extendEnLocale as Record<string, string>;
  }
  const [base, extend] = await Promise.all([loadBase(lang), loadExtend(lang)]);
  return { ...base, ...extend };
};

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  return (
    <LocaleProvider resources={resources}>
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

## Async resources (Next.js and webpack)

_Bundled SDK + extend JSON with webpack-friendly static imports. Pass an `AsyncResources` loader to `LocaleProvider`._

Use this pattern in **Next.js** when you bundle with **webpack** (default production build). Next.js does not provide Vite’s **`import.meta.glob`**. A dynamic import such as ``import(`@paxeer-network/i18n/locales/${lang}.json`)`` is also a poor fit: webpack must resolve **statically analyzable** subpaths under the package **`exports`** map, so the reliable approach is a **`Record<LocaleEnum, () => import(...)>`** with one explicit import per SDK locale. App extend files can still use a dynamic import from your app alias (e.g. ``import(`@/locales/${lang}.json`)``) because those paths live in your project tree.

- **Client component:** In the **App Router**, put the provider in a file with **`"use client"`** at the top when **`LocaleProvider`** runs on the client.
- **Base SDK strings:** import from **`@paxeer-network/i18n/locales/<locale>.json`** (published subpaths; add an entry for **every** `LocaleEnum` value you support).
- **Extend keys:** import or dynamic-import your app’s JSON (example: `@/locales/<lang>.json`—match your `tsconfig` paths).
- For **`en`**, return your imported extend file only (English base is built into the package).
- For other locales, `Promise.all` base + extend and merge. **`importLocaleJsonModule`** normalizes each module’s `default` export.

**Reference implementation:** `sdk-demo/src/components/paxeerNetworkProvider/PaxeerNetworkLocaleProvider.tsx` in the paxeer-network SDK template demo (same merge logic and full `Record<LocaleEnum, …>` base loaders; compare with [`paxeer-network-js-sdk-nextjs-template` / `components/paxeerNetworkProvider`](https://github.com/PaxeerNetworkNetwork/paxeer-network-js-sdk-nextjs-template/tree/main/components/paxeerNetworkProvider) for app wiring).

```tsx
"use client";

import React, { FC, ReactNode } from "react";
import {
  AsyncResources,
  importLocaleJsonModule,
  LocaleCode,
  LocaleEnum,
  LocaleProvider,
} from "@paxeer-network/i18n";
import type { LocaleJsonModule } from "@paxeer-network/i18n";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import extendEnLocale from "@/locales/en.json";

type LocaleJsonLoader = () => Promise<LocaleJsonModule>;

/**
 * One explicit import per locale so webpack can resolve @paxeer-network/i18n/locales/*.json
 * through package exports. Include every `LocaleEnum` value your app can switch to.
 */
const baseLocaleLoaders: Record<LocaleEnum, LocaleJsonLoader> = {
  [LocaleEnum.de]: () => import("@paxeer-network/i18n/locales/de.json"),
  [LocaleEnum.en]: () => import("@paxeer-network/i18n/locales/en.json"),
  [LocaleEnum.es]: () => import("@paxeer-network/i18n/locales/es.json"),
  [LocaleEnum.fr]: () => import("@paxeer-network/i18n/locales/fr.json"),
  [LocaleEnum.id]: () => import("@paxeer-network/i18n/locales/id.json"),
  [LocaleEnum.it]: () => import("@paxeer-network/i18n/locales/it.json"),
  [LocaleEnum.ja]: () => import("@paxeer-network/i18n/locales/ja.json"),
  [LocaleEnum.ko]: () => import("@paxeer-network/i18n/locales/ko.json"),
  [LocaleEnum.nl]: () => import("@paxeer-network/i18n/locales/nl.json"),
  [LocaleEnum.pl]: () => import("@paxeer-network/i18n/locales/pl.json"),
  [LocaleEnum.pt]: () => import("@paxeer-network/i18n/locales/pt.json"),
  [LocaleEnum.ru]: () => import("@paxeer-network/i18n/locales/ru.json"),
  [LocaleEnum.tc]: () => import("@paxeer-network/i18n/locales/tc.json"),
  [LocaleEnum.tr]: () => import("@paxeer-network/i18n/locales/tr.json"),
  [LocaleEnum.uk]: () => import("@paxeer-network/i18n/locales/uk.json"),
  [LocaleEnum.vi]: () => import("@paxeer-network/i18n/locales/vi.json"),
  [LocaleEnum.zh]: () => import("@paxeer-network/i18n/locales/zh.json"),
};

async function loadBase(lang: LocaleCode): Promise<Record<string, string>> {
  const loader = baseLocaleLoaders[lang as LocaleEnum];
  return importLocaleJsonModule(loader);
}

async function loadExtend(lang: LocaleCode): Promise<Record<string, string>> {
  return importLocaleJsonModule(() => import(`@/locales/${lang}.json`));
}

const resources: AsyncResources = async (lang: LocaleCode, _ns: string) => {
  if (lang === LocaleEnum.en) {
    return extendEnLocale as Record<string, string>;
  }
  const [base, extend] = await Promise.all([loadBase(lang), loadExtend(lang)]);
  return { ...base, ...extend };
};

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  return (
    <LocaleProvider resources={resources}>
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

## HTTP backend

_Locales under `public/` (or any URL your `loadPath` returns)._

### How HTTP loading works

`backend.loadPath` only returns URL(s). The internal `Backend` **`fetch`es** those URLs at runtime. **`@paxeer-network/i18n` does not install or copy** `dist/locales` into your app’s `public/` folder — you must **provide files** at those paths yourself.

### Getting built-in JSON into `public/`

**Manual:** Copy from `node_modules/@paxeer-network/i18n/dist/locales` (exact path depends on your install layout) into e.g. **`public/locales/`**. Put your app **extend** JSON under e.g. **`public/locales/extend/`** (same layout as `loadPath` below).

**Automate:** Use an npm script (e.g. `copyLocales`) and optionally a **Husky** hook (`pre-commit`, `postinstall`, or `prepare`) to run a small script that copies `dist/locales` → `public/locales`. The script below only covers **SDK** bundles; **extend** files usually stay in your repo and are committed or generated separately.

**CDN:** If `loadPath` points to a CDN, upload or deploy those JSON files there yourself — same idea: static URLs, not automatic sync from the package.

### Optional copy script (ESM)

Adjust `path.resolve` segments if your script lives in `scripts/` or in a monorepo package (paths differ from a single-package app). Add **`fs-extra`** as a dev dependency.

```js
import fs from "fs-extra";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function copyLocales() {
  await fs.copy(
    path.resolve(
      __dirname,
      "../node_modules/@paxeer-network/i18n/dist/locales",
    ),
    path.resolve(__dirname, "../public/locales"),
  );
}

copyLocales();
```

Wire it from **`package.json`**: `"scripts": { "copyLocales": "node ./scripts/copy-locales.mjs" }` (path as you prefer).

### Husky example (reference)

Example **`.husky/pre-commit`** hook:

```text
npm run copyLocales
git add public/locales/*.json
```

`copyLocales` runs your copy script; `git add` stages updated JSON so the commit includes them (adjust `glob` if you also version `public/locales/extend/`).

### `LocaleProvider` + `loadPath`

```typescript
import { FC, ReactNode } from "react";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import { LocaleProvider, LocaleEnum, LocaleCode } from "@paxeer-network/i18n";

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  // Requires public/locales/*.json and public/locales/extend/*.json (see sections above)
  const loadPath = (lang: LocaleCode, _ns: string) => {
    if (lang === LocaleEnum.en) {
      // because en is built-in, we need to load the en extend only
      return `/locales/extend/${lang}.json`;
    }
    return [`/locales/${lang}.json`, `/locales/extend/${lang}.json`];
  };

  return (
    <LocaleProvider backend={{ loadPath }}>
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

## Sync resources

_Imported JSON maps. Pass a static `Resources` map to `LocaleProvider`._

Use this when you bundle locale JSON (e.g. import from `@paxeer-network/i18n/locales/*`) and do not need async loading.

```typescript
import { FC, ReactNode } from "react";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import { LocaleProvider, Resources } from "@paxeer-network/i18n";
import zh from "@paxeer-network/i18n/locales/zh.json";
import ja from "@paxeer-network/i18n/locales/ja.json";
import ko from "@paxeer-network/i18n/locales/ko.json";

// extend or override English translations
const extendEn = {
  "extend.trading": "Trading",
};

// extend or override chinese translations
const extendZh = {
  "extend.trading": "交易",
};

// extend or override japanese translations
const extendJa = {
  "extend.trading": "取引",
};

// extend or override korean translations
const extendKo = {
  "extend.trading": "거래",
};

// define language resources
const resources: Resources = {
  en: extendEn,
  zh: {
    ...zh,
    ...extendZh,
  },
  ja: {
    ...ja,
    ...extendJa,
  },
  ko: {
    ...ko,
    ...extendKo,
  },
};

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  return (
    <LocaleProvider resources={resources}>
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

## Custom languages

_Subset of locales in the picker._

You can restrict the language list and provide only the locales you need via `languages` and `resources`. **`en`** strings are built into the package, so you do not need an `en` entry in `resources` unless you add extend keys; you can still list **`LocaleEnum.en`** in `languages` so the picker includes English.

```typescript
import { FC, ReactNode } from "react";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import {
  LocaleProvider,
  Resources,
  LocaleEnum,
  Language,
} from "@paxeer-network/i18n";

// japanese locale
const ja = {
  "extend.ja": "日本語",
};

// korean locale
const ko = {
  "extend.ko": "한국어",
};

// define language resources
const resources: Resources = {
  ja,
  ko,
};

// custom languages
const languages: Language[] = [
  { localCode: LocaleEnum.en, displayName: "English" },
  { localCode: LocaleEnum.ja, displayName: "日本語" },
  { localCode: LocaleEnum.ko, displayName: "한국어" },
];

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  return (
    <LocaleProvider resources={resources} languages={languages}>
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

## onLanguageChanged (URL sync)

_Keep the browser URL aligned with locale-prefixed routes when the user switches language (e.g. `/en/trade` → `/zh/trade`)._

Import **`LocaleCode`** and **`removeLangPrefix`** from `@paxeer-network/i18n` and pass the callback to **`LocaleProvider`** as **`onLanguageChanged`**. Strip the current locale segment from the pathname, then use **`history.replaceState`** to set the path to `/${lang}${path}`.

The example below matches [HTTP backend](#http-backend): the same **`loadPath`** (see that section for copying files under `public/`) and nested providers; **`onLanguageChanged`** adds URL sync on top.

```typescript
import { FC, ReactNode } from "react";
import { WalletConnectorProvider } from "@paxeer-network/wallet-connector";
import { PaxeerNetworkAppProvider } from "@paxeer-network/react-app";
import { LocaleProvider, LocaleEnum, LocaleCode, removeLangPrefix } from "@paxeer-network/i18n";

const PaxeerNetworkProvider: FC<{ children: ReactNode }> = (props) => {
  const onLanguageChanged = async (lang: LocaleCode) => {
    const path = removeLangPrefix(window.location.pathname);
    window.history.replaceState({}, "", `/${lang}${path}`);
  };

  // Please copy built-in locale files (@paxeer-network/i18n/locales) to your public/locales
  // and copy your extend locale files to public/locales/extend
  const loadPath = (lang: LocaleCode, _ns: string) => {
    if (lang === LocaleEnum.en) {
      // because en is built-in, we need to load the en extend only
      return `/locales/extend/${lang}.json`;
    }
    return [`/locales/${lang}.json`, `/locales/extend/${lang}.json`];
  };

  return (
    <LocaleProvider
      onLanguageChanged={onLanguageChanged}
      backend={{ loadPath }}
    >
      <WalletConnectorProvider>
        <PaxeerNetworkAppProvider
          brokerId="paxeer-network"
          brokerName="paxeer-network"
          networkId="testnet"
        >
          {props.children}
        </PaxeerNetworkAppProvider>
      </WalletConnectorProvider>
    </LocaleProvider>
  );
};
```

**Combining with async resources:** For **`AsyncResources`** and **`import.meta.glob`**, see [Async resources (Vite)](#async-resources-vite). For **`AsyncResources`** on webpack, see [Async resources (Next.js and webpack)](#async-resources-nextjs-and-webpack).

**Next.js App Router:** Prefer **`usePathname()`** from `next/navigation` (or your router’s path API) instead of **`window.location.pathname`** when stripping the locale inside **`onLanguageChanged`**. You may sync **`i18n`** with **`getLocalePathFromPathname`** when the URL locale changes.

**Framework routers:** **`history.replaceState`** updates the URL but does not always trigger the same lifecycle as your framework’s client navigation (e.g. Next.js `router`). If the framework exposes locale-aware navigation (**`router.push`**, **`Link`**, etc.), prefer that for in-app changes so router state stays consistent.

See also: [Integration guide](./integration.md) ([§1](./integration.md#1-wrap-your-app-with-localeprovider) · [§4](./integration.md#4-integrate-external-resources)) · [CLI](./cli.md)
