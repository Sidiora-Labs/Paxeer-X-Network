# VaultCardWidget

## Overview

Wrapper that connects a single `VaultInfo` to the vault card UI via `useVaultCardScript` and renders `VaultCard` with the script state.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| vault | VaultInfo | Yes | Vault to display |

## Usage example

```tsx
<VaultCardWidget vault={vaultInfo} />
```
