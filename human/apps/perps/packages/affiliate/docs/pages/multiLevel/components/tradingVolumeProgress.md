# pages/multiLevel/components/tradingVolumeProgress.tsx

## Responsibility of TradingVolumeProgress

Shows volume prerequisite progress: current vs required volume, progress bar, and a CTA button (e.g. go to trade). Uses useReferralContext for volumePrerequisite and useScaffoldContext for routerAdapter. Used on multi-level landing or affiliate page to unlock multi-level referral.

## TradingVolumeProgress Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| classNames | { root?, description?, button? } | No | Class overrides |
| buttonProps | ButtonProps | No | Button props |

## Dependencies

- react (FC, useMemo)
- @paxeer-network/i18n (useTranslation)
- @paxeer-network/ui (Button, ButtonProps, cn, Flex, parseNumber, Text)
- @paxeer-network/ui-scaffold (useScaffoldContext)
- @paxeer-network/utils (Decimal)
- ../../../provider (useReferralContext)

## TradingVolumeProgress Example

```tsx
<TradingVolumeProgress />
```
