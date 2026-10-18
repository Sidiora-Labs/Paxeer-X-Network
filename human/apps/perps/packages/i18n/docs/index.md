# packages/i18n/src — Documentation Index

**User guide (integration):** [../README.md](../README.md)

## Module responsibility

The `packages/i18n/src` directory provides internationalization (i18n) for paxeer-network applications: locale types, constants, React context and providers, i18next instance, backend loading, and path/locale utilities. It does not implement UI components; it exposes types, hooks, and providers for consuming apps.

## Key entities

| Entity                                          | Type           | Responsibility                                             | Entry             |
| ----------------------------------------------- | -------------- | ---------------------------------------------------------- | ----------------- |
| LocaleCode                                      | type           | Locale identifier (e.g. en, zh, ja)                        | types.ts          |
| LocaleMessages                                  | interface      | Base + extend translation keys for t()                     | types.ts          |
| LocaleEnum                                      | enum           | Supported locale codes                                     | constant.ts       |
| LocaleContext / useLocaleContext                | context + hook | Language list and change callbacks                         | context.ts        |
| I18nProvider                                    | component      | Thin wrapper over I18nextProvider                          | provider.tsx      |
| LocaleProvider                                  | component      | Wraps app with locale state, backend, resources            | provider.tsx      |
| i18n                                            | instance       | i18next instance with detector and resources               | i18n.ts           |
| useTranslation                                  | hook           | Translation hook bound to package i18n                     | useTranslation.ts |
| useLocaleCode                                   | hook           | Current locale code, reactive to language change           | useLocaleCode.ts  |
| Backend                                         | class          | Async load translation bundles by URL                      | backend.ts        |
| parseI18nLang / removeLangPrefix / generatePath | functions      | Locale from browser, path without locale, path with locale | utils.ts          |

## Directory structure

| Path                       | Description                                                                    |
| -------------------------- | ------------------------------------------------------------------------------ |
| [locale/](locale/index.md) | Locale message modules and English bundle                                      |
| (root)                     | Core types, constants, context, i18n instance, hooks, provider, backend, utils |

## Top-level files

| File                                   | Language | Responsibility                                   | Entry symbols                                                                                                                |
| -------------------------------------- | -------- | ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| [index.ts](index.ts.md)                | TS       | Re-exports public API                            | react-i18next, i18n, I18nProvider, LocaleProvider, useTranslation, useLocaleCode, context, types, constant, locale/en, utils |
| [types.ts](types.md)                   | TS       | Locale types and i18next augmentation            | LocaleCode, LocaleMessages, Resources                                                                                        |
| [constant.ts](constant.md)             | TS       | Locale enum, default languages, NS, storage keys | LocaleEnum, defaultLanguages, defaultLng, defaultNS                                                                          |
| [context.ts](context.md)               | TS       | Locale React context and hook                    | Language, LocaleContextState, LocaleContext, useLocaleContext                                                                |
| [i18n.ts](i18n.md)                     | TS       | i18next instance and resources                   | resources, i18n                                                                                                              |
| [useTranslation.ts](useTranslation.md) | TS       | useTranslation hook                              | useTranslation                                                                                                               |
| [useLocaleCode.ts](useLocaleCode.md)   | TS       | useLocaleCode hook                               | useLocaleCode                                                                                                                |
| [provider.tsx](provider.md)            | TSX      | I18nProvider and LocaleProvider                  | I18nProvider, LocaleProvider, I18nProviderProps, LocaleProviderProps                                                         |
| [utils.ts](utils.md)                   | TS       | Locale and path helpers                          | parseI18nLang, removeLangPrefix, getLocalePathFromPathname, generatePath                                                     |
| [backend.ts](backend.md)               | TS       | Backend for loading remote bundles               | Backend, BackendOptions                                                                                                      |
| [version.ts](version.md)               | TS       | Package version                                  | default export                                                                                                               |

## Search keywords

i18n, locale, language, translation, react-i18next, i18next, LocaleProvider, useTranslation, useLocaleCode, LocaleCode, LocaleMessages, Backend, parseI18nLang, generatePath, language switcher
