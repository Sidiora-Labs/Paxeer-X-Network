# button — Directory Index

## Directory Responsibility

Button components: primary Button (with variant/size/color), IconButton, ThrottledButton, and shared BaseButton used by other components.

## Files

| File                | Language   | Summary                                           | Link                                     |
| ------------------- | ---------- | ------------------------------------------------- | ---------------------------------------- |
| base.tsx            | TSX        | BaseButton and BaseButtonProps                    | [base.md](base.md)                       |
| button.tsx          | TSX        | Button with buttonVariants (variant, size, color) | [button.md](button.md)                   |
| iconButton.tsx      | TSX        | IconButton component                              | [iconButton.md](iconButton.md)           |
| throttledButton.tsx | TSX        | ThrottledButton for rate-limited clicks           | [throttledButton.md](throttledButton.md) |
| index.ts            | TypeScript | Re-exports all button components and ButtonProps  | [index.md](index.md)                     |

## Key Entities

| Entity          | File                | Role                                                           |
| --------------- | ------------------- | -------------------------------------------------------------- |
| Button          | button.tsx          | Main button with text/outlined/contained/gradient, size, color |
| BaseButton      | base.tsx            | Shared base for Button and others                              |
| IconButton      | iconButton.tsx      | Button for icon-only actions                                   |
| ThrottledButton | throttledButton.tsx | Button with click throttling                                   |
