# useTranslation.ts

## useTranslation.ts responsibility

Re-exports a useTranslation hook that uses the package i18n instance (or the one from I18nContext when provided). Subscribes to the store with bindI18nStore: "added" so components re-render when new language resources are loaded.

## useTranslation.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| useTranslation | function | Hook | useTranslation(ns?, options?) from react-i18next with package i18n |

## useTranslation parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| ns | FlatNamespace \| $Tuple<FlatNamespace> \| undefined | no | Namespace(s) |
| options | UseTranslationOptions | no | Override options; i18n from context or package default, bindI18nStore: "added" |

## useTranslation dependency and flow

- **Upstream**: react-i18next (I18nContext, useTranslation as _useTranslation), i18n.ts.
- **Flow**: Reads I18nContext; uses context?.i18n or package i18n; calls _useTranslation with bindI18nStore: "added" so added resources trigger re-render.

## useTranslation Example

```typescript
import { useTranslation } from "@paxeer-network/i18n";

function ConfirmButton() {
  const { t, i18n } = useTranslation();
  return <button>{t("common.confirm")}</button>;
}
```
