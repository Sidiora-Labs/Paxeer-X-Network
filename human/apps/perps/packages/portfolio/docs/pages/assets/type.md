# type.ts (assets)

## type.ts responsibility

Defines types for the assets/convert feature: converted assets map, convert transaction, convert record, and related enums/constants. Used by convert page and API response typing.

## type.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| ConvertedAssets | interface | Map | `[asset: string]: number` |
| ConvertTransaction | interface | Transaction row | venue, converted_asset, received_asset, qty, haircut, chain_id?, tx_id?, result? |
| ConvertRecord | interface | Convert record | convert_id, converted_asset, received_asset, received_qty, type, status, created_time, updated_time, details |
| ConvertType | type | Literal | "auto" \| "manual" |
| ConvertStatus | type | Literal | "completed" \| "pending" \| "failed" \| "cancelled" |
| VenueType | type | Literal | "on_chain" \| "internal_fund" |
| PAXEER_NETWORK_ASSETS_VISIBLE_KEY | constant | Storage key | "paxeer-network_assets_visible" |

## ConvertTransaction fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| transaction_id | number | Yes | Id |
| venue | "on_chain" \| "internal_fund" | Yes | Source |
| converted_asset | string | Yes | Asset converted from |
| received_asset | string | Yes | Asset received |
| converted_qty | number | Yes | Amount converted |
| received_qty | number | Yes | Amount received |
| haircut | number | Yes | Haircut applied |
| chain_id | number | No | For on_chain |
| tx_id | string | No | For on_chain |
| result | string | No | Result |

## ConvertRecord fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| convert_id | number | Yes | Record id |
| converted_asset | ConvertedAssets | Yes | Map of converted amounts |
| received_asset | string | Yes | Received asset |
| received_qty | number | Yes | Received quantity |
| type | "auto" \| "manual" | Yes | Convert type |
| status | ConvertStatus | Yes | Status |
| created_time | number | Yes | Timestamp |
| updated_time | number | Yes | Timestamp |
| details | ConvertTransaction[] | Yes | Transaction list |

## type.ts Example

```typescript
import type { ConvertRecord, ConvertTransaction, ConvertStatus } from "./type";

const record: ConvertRecord = {
  convert_id: 1,
  converted_asset: { USDC: 100 },
  received_asset: "USDC",
  received_qty: 99,
  type: "manual",
  status: "completed",
  created_time: Date.now(),
  updated_time: Date.now(),
  details: [],
};
```
