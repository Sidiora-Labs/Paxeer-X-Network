# listView.tsx

## listView.tsx 的职责

This file provides listView (component or utility). See directory index for context and exports.

## listView Example

```ts
// Import from @paxeer-network/ui or relative path as needed.
```

## Props

### `keyExtractor?: (item: T, index: number) => React.Key`

Extracts a stable React key from each data item. The key must be unique among
items and stable across reorders and removals — use a business identifier such
as `symbol`, `order_id`, etc. When omitted, the array index is used as the key,
which is only safe for static or append-only lists with stateless rows. Pass
`keyExtractor` whenever rows can be removed or reordered mid-list and rows hold
local UI state (dialogs, popovers, inputs).
