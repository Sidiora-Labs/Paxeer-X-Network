# toast — Directory Index

## Directory Responsibility

Toast notifications: Toaster component, toastTile, and toast API (re-export of react-hot-toast). Used for imperative toast messages.

## Files

| File          | Language   | Summary                           | Link                         |
| ------------- | ---------- | --------------------------------- | ---------------------------- |
| index.ts      | TypeScript | Re-exports Toaster, toast, etc.   | [index.md](index.md)         |
| Toaster.tsx   | TSX        | Toaster container component       | [Toaster.md](Toaster.md)     |
| toastTile.tsx | TSX        | ToastTile for individual toast UI | [toastTile.md](toastTile.md) |

## Key Entities

| Entity    | File            | Role                    |
| --------- | --------------- | ----------------------- |
| Toaster   | Toaster.tsx     | Renders toast container |
| toast     | react-hot-toast | Imperative toast() API  |
| ToastTile | toastTile.tsx   | Single toast item UI    |
