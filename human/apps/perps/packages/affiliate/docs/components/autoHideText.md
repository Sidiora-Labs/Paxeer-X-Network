# components/autoHideText.tsx

## Responsibility of AutoHideText

Truncates long text to fit container width: shows start and end with "..." in the middle. Can use visibleCount for a fixed character count or measure width (canvas) to compute visible chars. Full text is in a hidden span for measurement; displayed span shows truncated text. Used for addresses or long codes in tables.

## AutoHideText Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| text | string | Yes | Full text |
| className | string | No | Container class |
| visibleCount | number | No | If set, use fixed char count for truncation; otherwise measure by width |

## Dependencies

- react (FC, useEffect, useMemo, useRef, useState)
- @paxeer-network/ui (cn)

## AutoHideText Example

```tsx
<AutoHideText text="0x1234...abcd" className="w-32" />
<AutoHideText text={address} visibleCount={12} />
```
