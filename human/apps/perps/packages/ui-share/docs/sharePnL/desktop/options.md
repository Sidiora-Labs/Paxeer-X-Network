# options

## Overview

Single optional-field toggle: checkbox plus localized label (openPrice, closePrice, openTime, closeTime, markPrice, quantity, leverage). Clicking the row or checkbox toggles the option in the parent’s `Set<ShareOptions>`.

## Component

### ShareOption

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| type | ShareOptions | Yes | Which option (e.g. "openPrice"). |
| curType | Set<ShareOptions> | Yes | Currently selected set. |
| setShareOption | (fn: (prev: Set<ShareOptions>) => Set<ShareOptions>) => void | Yes | Updater for the set. |

Renders `Checkbox` and `Text` with i18n label for the option. Toggling adds/removes `type` from the set.

## Usage example

```tsx
<ShareOption
  type="openPrice"
  curType={shareOption}
  setShareOption={setShareOption}
/>
```
