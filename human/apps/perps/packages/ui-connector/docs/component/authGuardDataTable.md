# authGuardDataTable.tsx

## Overview

`AuthGuardDataTable` is a `DataTable` that uses `useDataTap` with account status and shows an auth guard as the empty view when the user is not at the required status or on the wrong network. Supports custom descriptions and labels for the guard.

## Exports

### `AuthGuardDataTable<RecordType>`

Generic component with props extending `DataTableProps<RecordType>` and guard-related props.

#### Props (guard-related)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `status` | `AccountStatusEnum` | No | Required account status (default derived from state) |
| `labels` | `alertMessages` | No | Button labels for guard fallback |
| `description` | `alertMessages` | No | Descriptions for guard fallback |
| `classNames.authGuardDescription` | `string` | No | Class for guard description area |
| `ignoreLoadingCheck` | `boolean` | No | Passed to DataTable; also true when wrong network or status not met |

Other `DataTableProps` (e.g. `dataSource`, `columns`, `manualPagination`) are passed through. Empty state is the guard view (connect/sign-in/enable trading/switch chain).

## Usage example

```tsx
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";

<AuthGuardDataTable
  dataSource={orders}
  columns={columns}
  status={AccountStatusEnum.EnableTrading}
  labels={{ connectWallet: "Connect to view orders" }}
>
  <ExtensionSlot position={ExtensionPositionEnum.EmptyDataIdentifier} />
</AuthGuardDataTable>
```
