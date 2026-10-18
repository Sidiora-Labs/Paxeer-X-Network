# index.ts

## index.ts responsibility

Re-exports the public API of the i18n package: react-i18next, i18next instance and factory, providers, hooks, context, types, constants, default English locale, and path/locale utilities.

## index.ts exports

| Name | Type | Role | Source |
|------|------|------|--------|
| (react-i18next) | module | Re-export | react-i18next |
| i18next, createInstance | default/named | Re-export | i18next |
| i18n | default | Package i18next instance | ./i18n |
| I18nProvider, LocaleProvider, I18nProviderProps, LocaleProviderProps | component / types | Providers | ./provider |
| useTranslation | function | Hook | ./useTranslation |
| useLocaleCode | function | Hook | ./useLocaleCode |
| (context) | all | LocaleContext, useLocaleContext, types | ./context |
| (types) | all | LocaleCode, LocaleMessages, Resources | ./types |
| (constant) | all | LocaleEnum, defaultLanguages, etc. | ./constant |
| (locale/en) | all | en | ./locale/en |
| (utils) | all | parseI18nLang, removeLangPrefix, generatePath, etc. | ./utils |

## index.ts dependency and usage

- **Upstream**: types (side-effect), i18n, provider, useTranslation, useLocaleCode, context, types, constant, locale/en, utils.
- **Downstream**: Applications import from `@paxeer-network/i18n`.

## index.ts Example

```typescript
import {
  useTranslation,
  useLocaleCode,
  LocaleProvider,
  LocaleEnum,
  parseI18nLang,
  en,
} from "@paxeer-network/i18n";

function App() {
  const { t } = useTranslation();
  const locale = useLocaleCode();
  return (
    <LocaleProvider supportedLanguages={[LocaleEnum.en, LocaleEnum.zh]}>
      <div>{t("common.confirm")}</div>
    </LocaleProvider>
  );
}
```
