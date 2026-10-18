# carousel (desktop)

## Overview

Horizontal carousel of background image thumbnails for the desktop Share PnL poster. Uses Embla Carousel; prev/next buttons and clicking a thumbnail update the selected index. Selected item has outline styling.

## Component

### CarouselBackgroundImage

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| backgroundImages | ReadonlyArray<string> \| string[] | Yes | Image URLs. |
| selectedSnap | number | Yes | Current selected index. |
| setSelectedSnap | (index: number) => void | Yes | Called when selection changes. |

Uses `useEmblaCarousel` with `containScroll: "keepSnaps"`, `dragFree: true`. Prev/Next buttons scroll or decrement/increment index when at bounds. Each thumbnail is 162px wide; selected has `oui-outline-primary-darken`.

## Usage example

```tsx
<CarouselBackgroundImage
  backgroundImages={shareOptions?.backgroundImages ?? []}
  selectedSnap={selectedSnap}
  setSelectedSnap={setSelectedSnap}
/>
```
