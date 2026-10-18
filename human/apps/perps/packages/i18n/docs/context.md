# context.ts

## context.ts responsibility

Provides React context for locale configuration: supported languages list, callbacks for before/after language change, and optional popup options for the language switcher UI. Consumed by LocaleProvider (which sets the value) and by switcher components via useLocaleContext.

## context.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Language | type | Language option | { localCode: LocaleCode; displayName: string } |
| PopupMode | type | Popup display mode | "modal" \| "dropdown" \| "sheet" |
| PopupProps | type | Popup config | mode?, className?, style? |
| LocaleContextState | type | Context value | languages, onLanguageBeforeChanged, onLanguageChanged, popup? |
| LocaleContext | context | React context | createContext<LocaleContextState> |
| useLocaleContext | function | Hook | useContext(LocaleContext) |

## LocaleContextState fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| languages | Language[] | yes | List of selectable languages |
| onLanguageBeforeChanged | (lang: LocaleCode) => Promise<void> | yes | Called before switching; used to load resources |
| onLanguageChanged | (lang: LocaleCode) => Promise<void> | yes | Called after language change |
| popup | PopupProps | no | Options for language switcher popup |

## context.ts dependency

- **Upstream**: types (LocaleCode).
- **Downstream**: constant (Language[]), provider.tsx.

## context.ts Example

```typescript
import { useLocaleContext } from "@paxeer-network/i18n";

function LanguageSwitcher() {
  const { languages, onLanguageBeforeChanged, onLanguageChanged } = useLocaleContext();
  const handleChange = async (lang) => {
    await onLanguageBeforeChanged(lang);
    await i18n.changeLanguage(lang);
    await onLanguageChanged(lang);
  };
  return (/* render languages and call handleChange */);
}
```
