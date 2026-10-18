# chainSelector.widget.tsx

## Overview

Composes the chain selector script and UI into a single widget and registers it as a simple dialog and a simple sheet in `@paxeer-network/ui`. Exports the widget, its props type, and the dialog/sheet IDs used to open it.

## Exports

### ChainSelectorWidgetProps

Props for the widget. Intersection of:

- **UseChainSelectorScriptOptions** (from `./chainSelector.script`): `networkId`, `bridgeLessOnly`, `close`, `resolve`, `reject`, `onChainChangeBefore`, `onChainChangeAfter`
- **Pick&lt;ChainSelectorProps, "isWrongNetwork" \| "variant" \| "className"&gt;** from `./chainSelector.ui`

So the widget accepts all script options plus UI props: `isWrongNetwork`, `variant` (`wide` | `compact`), `className`.

### ChainSelectorWidget

Function component that:

1. Calls `useChainSelectorScript(props)` to get state.
2. Renders `ChainSelector` with that state and passes through `variant` and `isWrongNetwork`.

### ChainSelectorDialogId

Constant `"ChainSelectorDialogId"`. Used with `openSimpleDialog(ChainSelectorDialogId, ...)` to open the chain selector in a large dialog, with title from `i18n.t("connector.switchNetwork")`, `variant: "wide"`, `isWrongNetwork: true`.

### ChainSelectorSheetId

Constant `"ChainSelectorSheetId"`. Used with `openSimpleSheet(ChainSelectorSheetId, ...)` to open the chain selector in a sheet, same title, `variant: "compact"`, `isWrongNetwork: true`, and custom content/body classes for background.

## Registration

- **Dialog**: `registerSimpleDialog(ChainSelectorDialogId, ChainSelectorWidget, { size: "lg", title, variant: "wide", isWrongNetwork: true })`.
- **Sheet**: `registerSimpleSheet(ChainSelectorSheetId, ChainSelectorWidget, { title, classNames, variant: "compact", isWrongNetwork: true })`.

## Usage example

```tsx
import {
  ChainSelectorWidget,
  ChainSelectorDialogId,
  ChainSelectorSheetId,
} from "@paxeer-network/ui-chain-selector";
import { openSimpleDialog, openSimpleSheet } from "@paxeer-network/ui";

// Use as standalone component
<ChainSelectorWidget
  variant="wide"
  isWrongNetwork={true}
  onChainChangeAfter={(chainId, state) => {}}
/>;

// Open as dialog
openSimpleDialog(ChainSelectorDialogId, { ... });

// Open as sheet
openSimpleSheet(ChainSelectorSheetId, { ... });
```
