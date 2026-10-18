# components

## Directory Responsibility

Shared UI components used across affiliate pages: AutoHideText (truncate with tooltip), EditCode button, PinButton. Not layout or provider; presentational and interaction only.

## Files

| File | Language | Summary | Entry symbols | Link |
|------|----------|---------|---------------|------|
| autoHideText.tsx | TSX | Truncates text and shows full value on hover/tooltip | AutoHideText | [autoHideText.md](autoHideText.md) |
| editCodeBtn.tsx | TSX | Button to trigger edit referral code | EditCode | [editCodeBtn.md](editCodeBtn.md) |
| pinButton.tsx | TSX | Pin/unpin button | PinBtn | [pinButton.md](pinButton.md) |

## Key Entities

| Entity | File | Responsibility |
|--------|------|----------------|
| AutoHideText | autoHideText.tsx | Long text with overflow hidden and full text on hover |
| editCodeBtn | editCodeBtn.tsx | Edit code action button |
| PinBtn | pinButton.tsx | Pin/unpin action button |
