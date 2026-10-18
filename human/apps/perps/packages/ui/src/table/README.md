# Multi-Field Sorting Feature

This feature allows independent sorting for multiple fields within a single table column header. Each field maintains its own sort state and can be sorted independently.

## MultiSort: Independent Field Sorting

The MultiSort feature allows multiple independent sortable fields within a single column header. The implementation is elegantly integrated at the Transform layer for optimal performance and maintainability.

### Core Architecture

```
User Click → MultiSortHeader → Transform.columns (onSort) → DataTable → Transform.dataSource → Sorted Data
```

### Key Features

- **Transform Layer Integration**: Sorting logic is cleanly separated in the Transform layer
- **Automatic Field Detection**: DataTable automatically detects multiSort fields and handles them appropriately
- **Enhanced String Sorting**: Intelligent sorting for complex formats like "PERP_1000PEPE_USDC"
- **Nested Field Support**: Sort by deep object properties (e.g., "user.profile.name")
- **Manual Sorting Mode**: Automatically switches to manual sorting for multiSort fields
- **Zero Configuration**: Works out of the box with minimal setup

### Basic Usage

```typescript
const columns: Column<MarketData>[] = [
  {
    title: "Market Info",
    dataIndex: "marketData",
    multiSort: {
      fields: [
        { sortKey: "symbol", label: "Symbol" },
        { sortKey: "volume24h", label: "24h Volume" },
        { sortKey: "price", label: "Price" }
      ]
    },
    render: (_, record) => (
      <div>
        <div>{record.symbol}</div>
        <div>{record.volume24h}</div>
        <div>{record.price}</div>
      </div>
    )
  }
];
```

### Implementation Details

#### 1. Transform Layer Architecture

**Transform.columns()**: Provides default onSort handlers for multiSort fields

```typescript
// Auto-generated onSort handler
onSort: (fieldKey: string, sortOrder?: SortOrder) => {
  setSorting([{ id: fieldKey, desc: sortOrder === "desc" }]);
};
```

**Transform.dataSource()**: Handles data sorting with optimized comparator

```typescript
// Efficient sorting with enhanced string/numeric support
const sortedData = [...dataSource].sort(
  createMultiSortComparator(sortId, currentSort.desc),
);
```

#### 2. Manual Sorting Detection

DataTable automatically detects multiSort fields and switches to manual sorting:

```typescript
const isMultiSortField = columns.some((col) =>
  col.multiSort?.fields?.some((field) => field.sortKey === sortId),
);
// Returns true → manual sorting, false → tanstack table sorting
```

#### 3. Enhanced Sorting Logic

- **Null Handling**: Null values always sort to bottom
- **Type Detection**: Automatic numeric vs string detection
- **String Enhancement**: `localeCompare` with `numeric: true` for complex strings
- **Nested Access**: Deep object property access with fallback

### Advanced Usage

#### Custom Sort Handler

```typescript
multiSort: {
  fields: [
    { sortKey: "symbol", label: "Symbol" },
    { sortKey: "volume", label: "Volume" }
  ],
  onSort: (fieldKey: string, sortOrder?: SortOrder) => {
    // Custom server-side sorting
    fetchSortedData(fieldKey, sortOrder);
  }
}
```

#### Nested Field Sorting

```typescript
multiSort: {
  fields: [
    { sortKey: "user.profile.name", label: "User" },
    { sortKey: "stats.trading.volume", label: "Volume" },
  ];
}
```

### Performance Optimizations

- **Efficient Comparator**: Single-pass sorting with optimized comparison logic
- **Memoized Processing**: Transform functions are memoized for performance
- **Smart Mode Switching**: Only uses manual sorting when needed
- **Minimal Re-renders**: Clean separation of concerns reduces unnecessary updates

### Benefits

✅ **Clean Architecture**: Logic separated into appropriate layers  
✅ **Minimal Configuration**: Works automatically with standard table setup  
✅ **High Performance**: Optimized sorting algorithms  
✅ **Type Safety**: Full TypeScript support  
✅ **Extensible**: Easy to customize with onSort handlers  
✅ **Maintainable**: Clear separation of concerns

### Migration

No breaking changes - existing table implementations continue to work unchanged. Simply add `multiSort` configuration to enable the feature.

```typescript
// Before: Single field sorting
{
  title: "Symbol",
  dataIndex: "symbol",
  onSort: true
}

// After: Multi-field sorting
{
  title: "Market Data",
  dataIndex: "marketData",
  multiSort: {
    fields: [
      { sortKey: "symbol", label: "Symbol" },
      { sortKey: "volume", label: "Volume" }
    ]
  }
}
```
