# checkbox

## Overview

Simple controlled checkbox with custom SVG icons for checked and unchecked states. Wraps a button that toggles on click.

## Component

### Checkbox

#### Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| size | number | No | 16 | Icon size (width/height). |
| className | string | No | — | Root button class. |
| checked | boolean | Yes | — | Checked state. |
| onCheckedChange | (checked: boolean) => void | Yes | — | Called with new value on toggle. |

## Usage example

```tsx
<Checkbox
  size={16}
  checked={checked}
  onCheckedChange={setChecked}
  className="oui-pt-[2px]"
/>
```
