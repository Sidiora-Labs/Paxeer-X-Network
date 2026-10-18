# tpslAdvanced.ui

## Overview

Presents the advanced TPSL form: back link, order info, Buy/Sell toggle, position type (Full/Partial), TP/SL input rows, PnL info, and Cancel/Submit buttons. Uses `useTPSLAdvanced` return type as props and shows SL price warning when applicable.

## Exports

### TPSLAdvancedUI

React component. Props = `ReturnType<typeof useTPSLAdvanced>` (formattedOrder, symbolInfo, setValue, setValues, metaState, slPriceError, estLiqPrice, onSubmit, onClose, etc.). Renders OrderInfo, side buttons, TPSLPositionTypeWidget, TPSLInputRowWidget for TP and SL, PnlInfo, and action buttons.

### ArrowRightIcon

SVG icon component (arrow left) used for the “back” link. Accepts `SVGProps<SVGSVGElement>`.

## Usage example

```tsx
const state = useTPSLAdvanced(props);
<TPSLAdvancedUI {...state} />;
```
