# editSheet

## Overview

Mobile edit order sheet: full-screen or bottom sheet with header, quantity/price/trigger/trailing inputs, and confirm. Includes widget, ui, script, and subcomponents (editSheetHeader, priceInput, quantityInput, quantitySlider, triggerPriceInput, trailingCallbackInput, activitedPriceInput, editDialogContent, editSheetContext). Hooks: useEditOrderEntry, useEditOrderMaxQty.

## Files

| File | Description |
|------|-------------|
| editSheet.widget.tsx | Widget entry. |
| editSheet.ui.tsx | Sheet UI layout. |
| editSheet.script.tsx | Sheet state and submit. |
| editSheetHeader.tsx | Sheet header. |
| priceInput.tsx | Price input. |
| quantityInput.tsx | Quantity input. |
| quantitySlider.tsx | Quantity slider. |
| triggerPriceInput.tsx | Trigger price input. |
| trailingCallbackInput.tsx | Trailing callback input. |
| activitedPriceInput.tsx | Activated price input. |
| editDialogContent.tsx | Dialog content. |
| editSheetContext.ts | Context for sheet. |
| hooks/useEditOrderEntry.ts | Edit order entry hook. |
| hooks/useEditOrderMaxQty.ts | Max quantity hook. |
