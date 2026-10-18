# title.script

## Overview

Defines title configuration type and a hook that returns it from `TradingRewardsContext`.

## Exports

### TitleConfig (type)

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| title | string \| ReactNode | No | Default: i18n "Trading rewards". |
| subtitle | string \| ReactNode | No | Default: i18n subtitle with brokerName. |
| content | string \| ReactNode | No | Default: "Learn more" link. |
| docOpenOptions | { url?, target?, features? } | No | Default: trading-rewards docs URL, _blank. |
| brokerName | string | No | Used in default subtitle. |

### useTitleScript

Returns `TitleConfig` from `useTradingRewardsContext().titleConfig`.

## Usage example

```tsx
const config = useTitleScript();
<Title {...config} />
```
