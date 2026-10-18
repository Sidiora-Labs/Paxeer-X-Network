# provider.tsx

## provider.tsx responsibility

Provides two React providers: I18nProvider (pass-through to I18nextProvider) and LocaleProvider. LocaleProvider wires locale/resources/backend, manages language list, runs backend load on init and before language change, and supplies LocaleContext (languages, onLanguageBeforeChanged, onLanguageChanged, popup).

## provider.tsx exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| I18nProvider | component | Wrapper | FC<I18nProviderProps>, forwards to I18nextProvider |
| LocaleProvider | component | Full locale setup | Children, locale/resources, backend, supportedLanguages, callbacks, LocaleContext |
| I18nProviderProps | type | Props | Partial<I18nextProviderProps> |
| LocaleProviderProps | type | Props | children, locale?, resource?, resources?, supportedLanguages?, onLocaleChange?, convertDetectedLanguage?, backend?, plus Partial<LocaleContextState> |

## LocaleProviderProps fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| children | ReactNode | yes | App tree |
| locale | LocaleCode | no | Initial/controlled locale |
| resource | Record<string, string> | no | Single locale bundle |
| resources | Resources | no | Per-locale bundles |
| supportedLanguages | LocaleCode[] | no | Filter defaultLanguages |
| onLocaleChange | (locale: LocaleCode) => void | no | Deprecated; use onLanguageChanged |
| convertDetectedLanguage | (lang: string) => LocaleCode | no | Map detected language |
| backend | BackendOptions | no | loadPath for remote bundles |
| languages | Language[] | no | Custom language list (LocaleContextState) |
| onLanguageBeforeChanged | (lang) => Promise<void> | no | Called before change; backend loads here |
| onLanguageChanged | (lang) => Promise<void> | no | Called after change |
| popup | PopupProps | no | Language switcher popup options |

## LocaleProvider execution flow

1. If resources: addResourceBundle for each locale/ns.
2. If resource + locale: addResourceBundle(locale, defaultNS, resource).
3. If locale prop and !== i18n.language: changeLanguage(locale).
4. Derive languages from props.languages or supportedLanguages (filter defaultLanguages).
5. On mount: initLanguage — convertDetectedLanguage or parseI18nLang(i18n.language), backend.loadLanguage, then changeLanguage if needed.
6. languageBeforeChangedHandle: onLanguageBeforeChanged + backend.loadLanguage.
7. languageChangedHandle: onLanguageChanged + onLocaleChange.
8. Provide LocaleContext and I18nextProvider with i18n and defaultNS.

## provider.tsx Example

```tsx
import { LocaleProvider, LocaleEnum } from "@paxeer-network/i18n";

<LocaleProvider
  supportedLanguages={[LocaleEnum.en, LocaleEnum.zh]}
  backend={{
    loadPath: (lang, ns) => `https://cdn.example.com/locales/${lang}.json`,
  }}
>
  <App />
</LocaleProvider>
```
