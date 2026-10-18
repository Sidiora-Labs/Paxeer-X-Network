# layout/layout.widget.tsx

## Responsibility of AffiliateLayoutWidget

Composes layout logic and UI: uses useLayoutBuilder to get SideBarProps and passes them with children to AffiliateLayout. This is the main layout entry used by the affiliate app.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | ReactNode | Yes | PropsWithChildren |

## Dependencies

- ./layout.ui (AffiliateLayout)
- ./layout.script (useLayoutBuilder)

## AffiliateLayoutWidget Example

```tsx
import { AffiliateLayoutWidget } from "@paxeer-network/affiliate";

<AffiliateLayoutWidget>
  <PageContent />
</AffiliateLayoutWidget>
```
