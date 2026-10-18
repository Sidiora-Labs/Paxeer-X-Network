# index.ts (Package Entry)

## Responsibility of index.ts

Package entry file. Re-exports Dashboard, multiLevel, provider, and layout so consumers use a single import path.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| Dashboard | namespace | Re-export | `./pages/dashboard` |
| (multiLevel) | module | Re-export | `./pages/multiLevel` |
| (provider) | module | Re-export | `./provider` |
| (layout) | module | Re-export | `./layout` |

## Input and Output

- **Input**: None (entry only).
- **Output**: Namespace `Dashboard` and all exports from `pages/multiLevel`, `provider`, and `layout`.

## Dependencies

- `./pages/dashboard`
- `./pages/multiLevel`
- `./provider`
- `./layout`

## index.ts Example

```typescript
import { Dashboard, ReferralProvider, AffiliateLayoutWidget } from "@paxeer-network/affiliate";

// Use Dashboard pages, wrap app with ReferralProvider, render AffiliateLayoutWidget for layout.
```
