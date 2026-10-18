# components/pinButton.tsx

## Responsibility of PinBtn

Toggle button: shows PinnedIcon or UnPinIcon based on pinned. onClick receives the new pinned state (!pinned). Used to pin/unpin items (e.g. referral codes or rows).

## PinBtn Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| pinned | boolean | Yes | Current pinned state |
| size | number | No | Icon size (default 12) |
| onClick | (pinned: boolean) => void | No | Called with new state on click |

## Dependencies

- react (FC)
- ../utils/types (IconProps)

## PinBtn Example

```tsx
<PinBtn pinned={isPinned} onClick={setIsPinned} />
```
