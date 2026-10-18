# types.ts

## types.ts responsibility

Defines locale-related TypeScript types and augments the i18next module so that `t()` has typed keys and resources. Provides `LocaleCode`, `LocaleMessages`, and `Resources` for multi-locale bundles.

## types.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| LocaleCode | type | Locale identifier | `keyof typeof LocaleEnum \| (string & {})` |
| LocaleMessages | interface | Base + extend keys for t() | Extends EnType and ExtendLocaleMessages |
| Resources | type | Per-locale partial messages | `{ [key in LocaleCode]?: Partial<LocaleMessages & T> }` |
| CustomTypeOptions | module augmentation | i18next resources type | In declare module "i18next" |

## LocaleCode

- **Input**: Not applicable (type only).
- **Output**: String type representing a locale code (e.g. en, zh, ja).

## LocaleMessages

- **Input**: Inferred from `en` in locale/en.ts; extended by `Record<extend.${string}, string>`.
- **Output**: Interface used as the resources type for the default namespace so `t("common.confirm")` is type-checked.

## Resources

- **Input**: Generic `T` for extra keys; keys are LocaleCode.
- **Output**: Object type for per-locale message bundles (e.g. for LocaleProvider `resources` prop).

## types.ts dependency and augmentation

- **Upstream**: constant (LocaleEnum), locale/en (EnType).
- **Augmentation**: `declare module "i18next"` sets `CustomTypeOptions.resources` to `{ [defaultNS]: LocaleMessages }`.

## types.ts Example

```typescript
import type { LocaleCode, LocaleMessages, Resources } from "@paxeer-network/i18n";

const code: LocaleCode = "en";
const bundles: Resources = {
  en: { "common.confirm": "Confirm" },
  zh: { "common.confirm": "确认" },
};
```
