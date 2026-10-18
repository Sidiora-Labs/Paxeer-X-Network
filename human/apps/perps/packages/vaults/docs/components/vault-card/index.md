# vault-card

## Overview

Single vault card: title, description, status, TVL, all-time return, deposit/withdraw buttons, “View more” link. Logic is in `useVaultCardScript`; presentation in `VaultCard` (vaultCard.ui). Constants and broker icon URL helper in constants.

## Files

| File | Language | Description |
|------|----------|-------------|
| [vaultCard.widget](./vault-card-widget.md) | TSX | VaultCardWidget: takes vault, uses useVaultCardScript, renders VaultCard |
| [vaultCard.script](./vaultCard-script.md) | TypeScript | useVaultCardScript: LP info, balance, open deposit/withdraw modal, openVaultWebsite |
| [vaultCard.ui](./vaultCard-ui.md) | TSX | VaultCard presentational component; formatAllTimeReturn helper |
| [constants](./constants.md) | TypeScript | PAXEER_NETWORK_ICON, PAXEER_NETWORK_VAULT_TITLE/DESCRIPTION, getBrokerIconUrl |
| [index](./vault-card-index.md) | TypeScript | Re-exports widget, script, ui, constants |
