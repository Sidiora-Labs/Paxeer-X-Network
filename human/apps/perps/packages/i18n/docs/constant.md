# constant.ts

## constant.ts responsibility

Defines supported locale codes, default language list with display names, default namespace, and storage/cookie keys used by the language detector. Used by i18n instance, LocaleProvider, and language switchers.

## constant.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| LocaleEnum | enum | Locale codes | en, zh, ja, es, ko, vi, de, fr, ru, id, tr, it, pt, uk, pl, nl, tc |
| defaultLanguages | array | Language options | Language[] with localCode and displayName |
| defaultLng | LocaleEnum | Fallback locale | LocaleEnum.en |
| defaultNS | string | Default namespace | "translation" |
| i18nLocalStorageKey | string | Storage key | "paxeer-network_i18nLng" |
| i18nCookieKey | string | Cookie key | "paxeer-network_i18nLng" |

## LocaleEnum values

| Value | Description |
|-------|-------------|
| en | English |
| zh | Chinese |
| ja | Japanese |
| es | Spanish |
| ko | Korean |
| vi | Vietnamese |
| de | German |
| fr | French |
| ru | Russian |
| id | Indonesian |
| tr | Turkish |
| it | Italian |
| pt | Portuguese |
| uk | Ukrainian |
| pl | Polish |
| nl | Dutch |
| tc | Traditional Chinese |

## constant.ts dependency

- **Upstream**: context (Language type).
- **Downstream**: types, i18n.ts, provider.tsx, utils.ts, backend.

## constant.ts Example

```typescript
import { LocaleEnum, defaultLanguages, defaultLng, defaultNS, i18nLocalStorageKey } from "@paxeer-network/i18n";

const lang = LocaleEnum.zh;
const list = defaultLanguages; // [{ localCode: "en", displayName: "English" }, ...]
```
