# layout/context.tsx

## Responsibility of LayoutProvider

React context provider for layout-related state. Used when sidebar or layout state must be shared. Exact API not fully inferable from name alone; assume it wraps children and provides layout context.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| LayoutProvider | FC<PropsWithChildren> | Provider | Wraps children with layout context |

## LayoutProvider Example

Typically used above layout content when layout state is needed in tree.
