# rewardsTooltip

## Overview

Tooltip that shows broker name, current broker rewards, and “other paxeer-network DEX” rewards. Can wrap a custom trigger (e.g. text) or use a default info icon.

## Exports

### RewardsTooltipProps (type)

| Property | Type | Description |
|----------|------|-------------|
| brokerName | string \| undefined | Broker display name. |
| curRewards | number | Rewards for current broker. |
| otherRewards | number | Rewards for other brokers. |

### RewardsTooltip (component)

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| rewardsTooltip | RewardsTooltipProps | No | — | Data for tooltip content. |
| children | ReactElement | No | — | Custom trigger; default is info icon. |
| className | string | No | — | Tooltip container class. |
| arrowClassName | string | No | — | Arrow class. |
| align | "start" \| "center" \| "end" | No | — | Tooltip alignment. |

## Usage example

```tsx
<RewardsTooltip
  rewardsTooltip={{ brokerName: "Broker A", curRewards: 100, otherRewards: 50 }}
  align="center"
/>
```
