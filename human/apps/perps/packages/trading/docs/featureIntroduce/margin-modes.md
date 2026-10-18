---
name: feature-introduce-margin-modes
description: Cross vs isolated margin for order entry—`margin_mode` on orders, relation to `useMarginModes`, and UI hooks to switch mode before placing a trade.
---

# Margin modes (`CROSS` / `ISOLATED`)

Order payloads include **`margin_mode`** ([`MarginMode`](../../../types/src/order.ts): `CROSS` | `ISOLATED`). The trading shell and order entry read the active account / symbol margin setting and pass it into [`useOrderEntry`](../../../hooks/src/next/useOrderEntry/useOrderEntry.ts); defaults are defined in [`orderEntry.store.ts`](../../../hooks/src/next/useOrderEntry/orderEntry.store.ts) (default **`CROSS`**).

## Semantics (short)

- **Cross (`CROSS`)**: margin is shared across positions under cross-margin rules; liquidation depends on account-level equity.
- **Isolated (`ISOLATED`)**: margin is allocated per position/symbol scope; risk is bounded by isolated margin (product-specific rules apply).

Exact liquidation math is backend / product policy; this doc only ties **UI → field → hooks**.

## UI: switching margin mode

Typical selectors in `packages/ui-order-entry`:

| Step | Selector / id | Source |
| ---- | --------------- | ------ |
| Open margin entry | `data-testid="oui-testid-orderEntry-margin-mode"` | [`LeverageBadge.tsx`](../../../ui-order-entry/src/components/header/LeverageBadge.tsx) |
| Pick cross in sheet | `data-testid="oui-testid-marginModeSwitch-option-CROSS"` | [`marginModeSwitch.ui.tsx`](../../../ui-order-entry/src/components/marginModeSwitch/marginModeSwitch.ui.tsx) |
| Pick isolated | `data-testid="oui-testid-marginModeSwitch-option-ISOLATED"` | same |
| Settings shortcut — cross | `data-testid="oui-testid-marginModeSettings-set-cross"` | [`marginModeSettings.ui.tsx`](../../../ui-order-entry/src/components/marginModeSettings/marginModeSettings.ui.tsx) |
| Settings shortcut — isolated | `data-testid="oui-testid-marginModeSettings-set-isolated"` | same |

After switching, new orders should carry the selected `margin_mode` in the creator output.

## Related docs

- [Feature introduce index](./index.md)
- [Scenario matrix](./scenario-matrix.md) (96 `TASK-*` rows)
