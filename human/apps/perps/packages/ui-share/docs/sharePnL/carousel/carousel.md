# carousel

## Overview

Embla-based carousel with React context: provides ref, api, scrollPrev/Next, canScrollPrev/Next, selectedIndex, scrollSnaps. Exposes Carousel, CarouselContent, CarouselItem, CarouselPrevious, CarouselNext, CarouselIdentifier, and Dot. Supports horizontal/vertical and optional initIndex.

## Exports

### useCarousel

Hook to read carousel context. Must be used inside `<Carousel>`.

Returns: `carouselRef`, `api`, `scrollPrev`, `scrollNext`, `canScrollPrev`, `canScrollNext`, `selectedIndex`, `scrollSnaps`, plus props (opts, orientation).

### Carousel

ForwardRef component. Props: `opts?`, `plugins?`, `orientation?` ("horizontal" | "vertical"), `setApi?`, `initIndex?`, plus div attributes. Provides context and handles keyboard (ArrowLeft/ArrowRight).

### CarouselContent

ForwardRef; wraps children in the scroll container. Uses `carouselRef` and orientation for layout class.

### CarouselItem

ForwardRef; single slide (role="group", aria-roledescription="slide"). Uses orientation for spacing.

### CarouselPrevious / CarouselNext

Button components that call scrollPrev/scrollNext and are disabled when canScrollPrev/canScrollNext is false.

### CarouselIdentifierProps

| Property | Type | Description |
|----------|------|-------------|
| className | string | Optional. |
| dotClassName | string | Optional. |
| dotActiveClassName | string | Optional. |
| onClick | (index: number) => void | Optional. |

### Dot

| Prop | Type | Description |
|------|------|-------------|
| index | number | Slide index. |
| active | boolean | Whether selected. |
| onClick | (index: number) => void | Optional. |
| className | string | Optional. |
| activeClassName | string | Optional; default "oui-bg-primary-darken". |

### CarouselIdentifier

Renders a row of Dots from scrollSnaps; uses useCarousel().

## Usage example

```tsx
<Carousel opts={{ align: "start" }} initIndex={0}>
  <CarouselContent style={{ height: "200px" }}>
    {items.map((item, i) => (
      <CarouselItem key={i}>{item}</CarouselItem>
    ))}
  </CarouselContent>
  <CarouselIdentifier dotClassName="..." dotActiveClassName="..." />
</Carousel>
```
