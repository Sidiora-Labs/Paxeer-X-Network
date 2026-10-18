# ViewModeToggle

## Overview

Button that toggles between grid and list view. Renders an icon (list or grid) and calls `onChange` with the new mode on click.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| mode | "grid" \| "list" | Yes | Current view mode |
| onChange | (mode: "grid" \| "list") => void | Yes | Called when user toggles |

## Usage example

```tsx
<ViewModeToggle mode={viewMode} onChange={setViewMode} />
```
