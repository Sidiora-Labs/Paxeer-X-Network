# utils.ts

## utils.ts responsibility

Provides locale and path helpers: parse browser language to LocaleCode, remove locale prefix from pathname, get locale segment from pathname, and generate a path with locale prefix and optional search params.

## utils.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| parseI18nLang | function | Normalize locale | (lang, localeCodes?, defaultLang?) => LocaleCode |
| removeLangPrefix | function | Strip locale from path | (pathname, localeCodes?) => string |
| getLocalePathFromPathname | function | Get locale segment | (pathname, localeCodes?) => string \| null |
| generatePath | function | Path with locale + search | (params) => string |

## parseI18nLang parameters and behavior

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| lang | string | yes | Browser language (e.g. en-US, zh-CN) |
| localeCodes | LocaleCode[] | no | Allowed codes; default Object.values(LocaleEnum) |
| defaultLang | LocaleCode | no | Fallback; default LocaleEnum.en |

- Extracts 2-letter prefix with regex; if full lang in localeCodes returns it; else if prefix in localeCodes returns prefix; else returns defaultLang.

## removeLangPrefix / getLocalePathFromPathname

- **getLocalePathFromPathname**: pathname.split("/")[1]; if that segment is in localeCodes return it, else null.
- **removeLangPrefix**: If getLocalePathFromPathname returns a segment, remove `/${segment}` from pathname (only when followed by /); else return pathname unchanged.

## generatePath parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | yes | Base pathname |
| locale | string | no | Locale prefix; default i18n.language or parseI18nLang |
| search | string | no | Query string; default window.location.search |

- If path already has valid locale prefix (from getLocalePathFromPathname), return path + search.
- Else prepend `/${locale}` to path and append search.

## utils.ts dependency

- **Upstream**: constant (LocaleEnum), types (LocaleCode), i18n (for generatePath and parseI18nLang fallback).
- **Downstream**: provider (parseI18nLang in initLanguage).

## utils.ts Example

```typescript
import { parseI18nLang, removeLangPrefix, getLocalePathFromPathname, generatePath } from "@paxeer-network/i18n";

parseI18nLang("en-US"); // "en"
removeLangPrefix("/en/perp/PERP_ETH_USDC"); // "/perp/PERP_ETH_USDC"
getLocalePathFromPathname("/en/markets"); // "en"
generatePath({ path: "/markets", locale: "zh" }); // "/zh/markets" + search
```
