# components

## Overview

UI components for the order entry: header (side + order type + leverage), order type select, order inputs (limit/stop/scaled/trailing), quantity slider, TPSL, reduce-only, dialogs (confirm, max qty, scaled order confirm), fee widgets, slippage, additional options (FOK/IOC/Post-only), LTV risk tooltip, and supporting pieces.

## Subdirectories and files

| Path | Description |
|------|-------------|
| [additional/](additional/index.md) | PinButton, AdditionalInfo, AdditionalConfigButton |
| [advancedTPSLResult/](advancedTPSLResult/index.md) | AdvancedTPSLResult – display/edit/delete advanced TP/SL result |
| [assetInfo/](assetInfo/index.md) | AssetInfo – est liq price, leverage, slippage, fees |
| [available/](available/index.md) | Available – free collateral / available balance |
| [customInput/](customInput/index.md) | CustomInput component |
| [dialog/](dialog/index.md) | confirm.ui, maxQtyConfirm, scaledOrderConfirm |
| [fee/](fee/index.md) | FeesWidget, regular fee, effective fee |
| [header/](header/index.md) | OrderEntryHeader, LeverageBadge |
| [LTVRiskTooltip/](LTVRiskTooltip/index.md) | LTVRiskTooltip UI, script, widget |
| [orderInput/](orderInput/index.md) | OrderInput – limit/stop/scaled/trailing inputs, qty/total |
| [orderTypeSelect/](orderTypeSelect/index.md) | OrderTypeSelect |
| [pnlInput/](pnlInput/index.md) | PnL input provider, context, widget, UI |
| [quantitySlider/](quantitySlider/index.md) | QuantitySlider |
| [reduceOnlySwitch/](reduceOnlySwitch/index.md) | ReduceOnlySwitch |
| [slippage/](slippage/index.md) | Slippage UI, editor, cell |
| [tpsl.tsx](tpsl.md) | OrderTPSL – TP/SL switch and inputs |
| orderEntryProvider.tsx, orderEntryContext.tsx | See root [orderEntryProvider](../orderEntryProvider.md), [orderEntryContext](../orderEntryContext.md) |
