# box — Directory Index

## Directory Responsibility

Box layout primitive: polymorphic container (div/span/nav/section/etc.) with layout, shadow, decoration, position, and visible variants via `tailwind-variants` (tv). Entry for layout composition across the UI package.

## Files

| File     | Language   | Summary                       | Link                 |
| -------- | ---------- | ----------------------------- | -------------------- |
| box.tsx  | TSX        | Box component and boxVariants | [box.md](box.md)     |
| index.ts | TypeScript | Re-exports Box, boxVariants   | [index.md](index.md) |

## Key Entities

| Entity      | File    | Role                                                                     |
| ----------- | ------- | ------------------------------------------------------------------------ |
| Box         | box.tsx | Layout primitive with asChild, as, width, height, angle, layout variants |
| boxVariants | box.tsx | tv() variants for layout, shadow, decoration, position, visible          |
