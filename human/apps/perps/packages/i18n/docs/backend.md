# backend.ts

## backend.ts responsibility

Implements a simple backend that loads translation bundles from URLs (returned by loadPath). Fetches JSON, adds bundles via i18n.addResourceBundle, and caches loaded URLs to avoid duplicate requests. Used by LocaleProvider before language change to load remote locale files.

## backend.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| BackendOptions | type | Config | { loadPath: (lang, ns) => string \| string[] \| undefined } |
| Backend | class | Loader | options, cache; loadLanguage(lang, ns) |

## BackendOptions fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| loadPath | (lang: LocaleCode, ns: string) => string \| string[] \| undefined | yes | URL(s) to fetch for the locale/namespace |

## Backend class

| Member | Type | Description |
|--------|------|-------------|
| options | BackendOptions | Passed in constructor |
| cache | Set<string> | URLs already loaded |
| fetchData(url) | async => object | fetch(url).then(r => r.json()); on error returns {} |
| loadLanguage(lang, ns) | async | Calls loadPath; normalizes to string[]; filters URLs not yet loaded (or locale/ns not in i18n); fetches each, addResourceBundle, adds to cache |

## Backend loadLanguage flow

1. If loadPath is not a function, return.
2. paths = loadPath(lang, ns); if falsy or empty, return.
3. Normalize paths to array.
4. Filter: skip if i18n.hasResourceBundle(lang, ns) and url in cache.
5. For each URL: fetchData, i18n.addResourceBundle(lang, ns, data, true, true), cache.add(url).
6. Await all fetches.

## backend.ts Example

```typescript
import { Backend } from "@paxeer-network/i18n";

const backend = new Backend({
  loadPath: (lang, ns) => `https://cdn.example.com/locales/${lang}.json`,
});
await backend.loadLanguage("zh", "translation");
```
