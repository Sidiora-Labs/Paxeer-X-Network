# poster

## Overview

Canvas-based poster component. Uses `usePoster` from `@paxeer-network/hooks` with `DrawOptions`; exposes ref methods for download, toDataURL, toBlob, and copy (e.g. clipboard).

## Types

### PosterProps

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| width | number | Yes | Canvas width. |
| height | number | Yes | Canvas height. |
| className | string | No | Canvas class. |
| ratio | number | No | Pixel ratio (e.g. 3 for retina). |
| data | DrawOptions | Yes | Drawing config from hooks. |
| style | React.CSSProperties | No | Canvas style. |

### PosterRef

Imperative handle exposed via ref:

| Method | Signature | Description |
|--------|------------|-------------|
| download | (filename: string, type?: string, encoderOptions?: number) => void | Downloads canvas as image. |
| toDataURL | (type?: string, encoderOptions?: number) => string | Returns data URL. |
| toBlob | (type?: string, encoderOptions?: number) => Promise<Blob \| null> | Returns blob. |
| copy | () => Promise<void> | Copies image to clipboard. |

## Component

### Poster

ForwardRef component; renders a `<canvas>`. Ref receives the PosterRef object (download, toDataURL, toBlob, copy).

## Usage example

```tsx
const posterRef = useRef<PosterRef | null>(null);
<Poster
  width={552}
  height={310}
  ratio={3}
  data={{ backgroundImg, ...options, data: posterData }}
  ref={posterRef}
/>;
posterRef.current?.copy();
posterRef.current?.download("Poster.png");
```
