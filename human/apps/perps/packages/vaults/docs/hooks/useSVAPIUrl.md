# useSVAPIUrl

## Overview

Returns the strategy vault API base URL for the current environment. Uses `useGetEnv` from `@paxeer-network/hooks` and maps to `VAULTS_API_URLS[env]`.

## Exports

### useSVApiUrl()

- **Returns**: `string` — Base URL (e.g. `https://api.paxeer.network` for prod).
- **Dependencies**: `useGetEnv()`, `VAULTS_API_URLS` from `../api/env`.

## Usage example

```tsx
import { useSVApiUrl } from "../../hooks/useSVAPIUrl";
const apiUrl = useSVApiUrl();
fetchVaultInfo(undefined, apiUrl);
```
