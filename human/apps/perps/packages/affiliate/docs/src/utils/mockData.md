# mockData.ts

## Overview

Static mock data for development: `MockData.referralInfo` contains sample referrer_info and referee_info (invites, volume, referral_codes, rebates, etc.).

## Exports

| Export | Description |
|--------|-------------|
| `MockData` | Class with static `referralInfo` – object matching referral API shape |

## Usage Example

```ts
import { MockData } from "./mockData";
const mock = MockData.referralInfo;
```
