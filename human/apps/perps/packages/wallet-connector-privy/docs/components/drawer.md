# Drawer

## Overview

**Drawer** is a portal-based slide-out panel (via `createPortal`) that overlays the page. It locks body scroll when open and adjusts height for mobile vs desktop.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | `ReactNode` | Yes | Content of the drawer. |
| isOpen | `boolean` | Yes | Whether the drawer is visible. |
| onClose | `() => void` | Yes | Called to close the drawer. |

## Usage example

```tsx
<Drawer isOpen={open} onClose={() => setOpen(false)}>
  <div>Drawer content</div>
</Drawer>
```
