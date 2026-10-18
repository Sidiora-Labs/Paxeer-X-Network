# editIcon.tsx

## Overview

SVG edit/pencil icon. Used by EditCode and other edit actions. Accepts IconProps (size, className, onClick, etc.).

## Props

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| size | number | No | | Icon size |
| className | string | No | | CSS class |
| onClick | () => void | No | | Click handler |
| ...rest | SVGProps | No | | SVG props |

## Usage Example

```tsx
import { EditIcon } from "../components/editIcon";
<EditIcon size={16} onClick={() => {}} />
```
