# affiliateIcon.tsx

## Overview

SVG icon component for the affiliate role. Accepts `IconProps` (size, className, and SVG props). Default size 16.

## Props

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| size | number | No | 16 | Width and height |
| className | string | No | | CSS class |
| ...rest | SVGProps | No | | Passed to svg element |

## Usage Example

```tsx
import { AffiliateIcon } from "../components/affiliateIcon";
<AffiliateIcon size={24} className="icon" />
```
