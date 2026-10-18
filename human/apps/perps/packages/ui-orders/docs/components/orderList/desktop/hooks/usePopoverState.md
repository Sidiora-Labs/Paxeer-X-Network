# usePopoverState

## Overview

Hook that manages open/editing state for an editable cell popover: `open`, `editing`, `containerRef`, `closePopover`, `cancelPopover`, `onClick` (open when value !== originValue). Used for inline edit popovers in the order list.

## Parameters

| Name | Type | Description |
|------|------|-------------|
| `originValue` | `string` | Original value. |
| `value` | `string` | Current value. |
| `setValue` | `(value: string) => void` | Setter for value (e.g. reset on cancel). |
