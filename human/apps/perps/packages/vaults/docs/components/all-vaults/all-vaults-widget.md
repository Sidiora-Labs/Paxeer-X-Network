# AllVaultsWidget

## Overview

Entry component for the “all vaults” section. Uses `useVaultInfoState()` for vault list and `useScreen()` to render either `AllVaultsMobile` or `AllVaultsDesktop` with that data.

## Exports

### AllVaultsWidget

- **Type**: `FC` (no props).
- **Behavior**: Renders `AllVaultsMobile` or `AllVaultsDesktop` with `vaults={data}` from store.

## Usage example

```tsx
<AllVaultsWidget />
```
