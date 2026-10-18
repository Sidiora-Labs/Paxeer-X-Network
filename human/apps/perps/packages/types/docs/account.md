# account

## Overview

Default scope for the paxeer-network key. Used when adding or validating paxeer-network API key permissions.

## Exports

### DEFAUL_PAXEER_NETWORK_KEY_SCOPE

- **Type:** `string`
- **Value:** `"read,trading"`

Default scope string for the paxeer-network key (read and trading permissions).

## Usage example

```typescript
import { DEFAUL_PAXEER_NETWORK_KEY_SCOPE } from "@paxeer-network/types";
addPaxeerNetworkKey({ scope: DEFAUL_PAXEER_NETWORK_KEY_SCOPE, ... });
```
