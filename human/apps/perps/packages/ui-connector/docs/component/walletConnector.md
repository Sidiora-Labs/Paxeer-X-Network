# walletConnector.tsx

## Overview

Registers the wallet connector as a simple dialog and a simple sheet, and exports the widget component and modal/sheet IDs. The widget uses `useWalletConnectorBuilder` and renders `WalletConnectContent`.

## Exports

| Export | Type | Description |
|--------|------|-------------|
| `WalletConnectorModalId` | `"walletConnector"` | ID for desktop modal |
| `WalletConnectorSheetId` | `"walletConnectorSheet"` | ID for mobile sheet |
| `WalletConnectorWidget` | `React.FC<any>` | Widget that renders `WalletConnectContent` with builder state |

## Registration

- **Dialog**: `registerSimpleDialog(WalletConnectorModalId, WalletConnectorWidget, { size: "sm", title: () => i18n.t("connector.connectWallet") })`
- **Sheet**: `registerSimpleSheet(WalletConnectorSheetId, WalletConnectorWidget, { title: () => i18n.t("connector.connectWallet") })`

## Usage example

```tsx
import { modal } from "@paxeer-network/ui";
import { WalletConnectorModalId, WalletConnectorSheetId } from "@paxeer-network/ui-connector";

// Show modal (e.g. desktop)
modal.show(WalletConnectorModalId, { title: "Connect wallet" });

// Show sheet (e.g. mobile)
modal.show(WalletConnectorSheetId, { title: "Connect wallet" });
```
