# widget.ts

## widget.ts responsibility

Provides widget-level copy: asset history status (pending, confirm, processing, completed, failed, pending rebalance), link device (QR code loading/link mobile/success/copy URL, scan QR, tooltip, connected description), settle (settle PnL, warning, description, unsettled tooltip, settlement requested/completed/failed, error), language switcher (language, tooltip, AI translation tips), announcement types (listing, maintenance, delisting), maintenance dialog and tips, restricted info (description, access restricted, agree), sub-account modal (switch account, main/sub accounts, current, no account, create max/title/description/nickname/success/failed, edit title/nickname/success/failed), funding (funding fee, rate, annual rate, payment type paid/received), left nav feedback, error boundary (title, description, refresh), DMM maker/taker.

## widget.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| widget | object | Key-value map | Keys under "assetHistory.*", "linkDevice.*", "settle.*", "languageSwitcher.*", "announcement.*", "maintenance.*", "restrictedInfo.*", "subAccount.*", "funding.*", "leftNav.*", "errorBoundary.*", "dmm.*" |
| Widget | type | typeof widget | Type export |

## widget.ts Example

```typescript
t("settle.settlePnl");
t("languageSwitcher.language");
t("subAccount.modal.create.title");
```
