# modal — Directory Index

## Directory Responsibility

Modal system: useModal hook, ModalContext, presets (alert, confirm, sheet, dialog), and registration utilities. Used for imperative modals and composed overlays.

## Files

| File                                    | Language   | Summary                                    | Link                                   |
| --------------------------------------- | ---------- | ------------------------------------------ | -------------------------------------- |
| index.tsx                               | TSX        | Modal exports and root                     | [index.md](index.md)                   |
| modalContext.tsx                        | TSX        | ModalContext, modalActions, ModalIdContext | [modalContext.md](modalContext.md)     |
| modalHelper.tsx                         | TSX        | register and modal helpers                 | [modalHelper.md](modalHelper.md)       |
| useModal.tsx                            | TSX        | useModal hook (show/hide/remove)           | [useModal.md](useModal.md)             |
| types.ts                                | TypeScript | ModalArgs, ModalHandler types              | [types.md](types.md)                   |
| utils.ts                                | TypeScript | getModalId and modal utils                 | [utils.md](utils.md)                   |
| [preset/alert.tsx](preset/alert.md)     | TSX        | Alert modal preset                         | [preset/alert.md](preset/alert.md)     |
| [preset/confirm.tsx](preset/confirm.md) | TSX        | Confirm modal preset                       | [preset/confirm.md](preset/confirm.md) |
| [preset/dialog.tsx](preset/dialog.md)   | TSX        | Dialog modal preset                        | [preset/dialog.md](preset/dialog.md)   |
| [preset/sheet.tsx](preset/sheet.md)     | TSX        | Sheet modal preset                         | [preset/sheet.md](preset/sheet.md)     |

## Key Entities

| Entity        | File             | Role                                              |
| ------------- | ---------------- | ------------------------------------------------- |
| useModal      | useModal.tsx     | Hook to show/hide/remove modal by id or component |
| ModalContext  | modalContext.tsx | Holds modal state and actions                     |
| Modal presets | preset/\*.tsx    | alert, confirm, dialog, sheet                     |
