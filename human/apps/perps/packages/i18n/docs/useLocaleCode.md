# useLocaleCode.ts

## useLocaleCode.ts responsibility

Provides the current locale code as React state, updated when i18n fires "languageChanged". Used by components that need to react to language changes without using useTranslation.

## useLocaleCode.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| useLocaleCode | function | Hook | Returns current LocaleCode, reactive to languageChanged |

## useLocaleCode input and output

- **Input**: None.
- **Output**: LocaleCode (current i18n.language; state updates on languageChanged).

## useLocaleCode execution flow

1. Initialize state with i18n.language.
2. Subscribe to i18n.on("languageChanged") in useEffect.
3. On event, set state to new language.
4. Cleanup: i18n.off("languageChanged") on unmount.

## useLocaleCode Example

```typescript
import { useLocaleCode } from "@paxeer-network/i18n";

function LocaleDisplay() {
  const locale = useLocaleCode();
  return <span>{locale}</span>;
}
```
