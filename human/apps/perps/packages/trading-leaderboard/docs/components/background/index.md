# background

Leaderboard background component. Renders an image or video background with a gradient overlay; hidden on mobile.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.tsx` | TSX | LeaderboardBackground component | (this doc) |

## Exports

### `LeaderboardBackground`

#### Props (`BackgroundProps`)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `backgroundSrc` | `string` | No | URL to image or video (mp4, webm, ogg, avi) |

**Behavior:** Uses gradient overlay; video is muted, looped, autoplay. On mobile (`useScreen().isMobile`) returns `null`. Image/video type is inferred from URL extension.

## Usage example

```tsx
import { LeaderboardBackground } from "@paxeer-network/trading-leaderboard";

<LeaderboardBackground backgroundSrc="/background.mp4" />
```
