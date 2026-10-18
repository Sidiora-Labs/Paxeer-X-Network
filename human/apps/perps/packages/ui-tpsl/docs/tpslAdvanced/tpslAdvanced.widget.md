# tpslAdvanced.widget

## Overview

Widget that composes `useTPSLAdvanced` and `TPSLAdvancedUI`, and registers the advanced TPSL sheet and dialog with titles from i18n (`common.settings`).

## Exports

### Props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| order | `PaxeerNetworkOrder` | Yes | Order to edit |
| setOrderValue | `(key: string, value: any) => void` | Yes | Parent state setter |
| onSubmit | `(formattedOrder: PaxeerNetworkOrder) => void` | Yes | Submit callback with formatted order |
| onClose | `() => void` | Yes | Close callback |
| symbolLeverage | `number` | No | Leverage for display |

### TPSLAdvancedWidget

React component that renders the advanced TPSL form in a sheet or dialog.

### TPSLAdvancedSheetId / TPSLAdvancedDialogId

Constants used with `registerSimpleSheet` and `registerSimpleDialog`.

## Usage example

```tsx
import { TPSLAdvancedWidget, TPSLAdvancedDialogId } from "@paxeer-network/ui-tpsl";
import { modal } from "@paxeer-network/ui";

modal.show(TPSLAdvancedDialogId, {
  order: formattedOrder,
  setOrderValue: (key, value) => setOrder({ ...order, [key]: value }),
  onSubmit: (next) => { save(next); hide(); },
  onClose: hide,
});
```
