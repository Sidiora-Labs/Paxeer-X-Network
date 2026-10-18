# useVaultCardScript

## Overview

Hook that wires one vault to store (LP info fetch), account/collateral, and UI actions. Returns title, description, vaultInfo, lpInfo (deposits/earnings), connection flags, openDepositAndWithdraw, openVaultWebsite, availableBalance, and disabled state for buttons.

## Parameters

- **vault**: `VaultInfo` — The vault to bind.

## Returns (VaultCardScript)

| Field | Type | Description |
|-------|------|-------------|
| title | string | vault.vault_name |
| description | string | vault.description |
| icon | string | Broker icon URL from getBrokerIconUrl |
| vaultInfo | VaultInfo | Same vault |
| lpInfo | { deposits, earnings } | From store LP info or "--" |
| isEVMConnected | boolean | state.chainNamespace === "EVM" |
| isSOLConnected | boolean | state.chainNamespace === "SOL" |
| openDepositAndWithdraw | (tab: "deposit" \| "withdraw") => void | Opens modal/sheet |
| availableBalance | number | USDC holding from useCollateral |
| openVaultWebsite | () => void | Opens app vault page in new tab |
| isWrongNetwork | boolean | — |
| isButtonsDisabled | boolean | vault.status !== "live" |

## Usage example

```tsx
const state = useVaultCardScript(vault);
<VaultCard {...state} />;
```
