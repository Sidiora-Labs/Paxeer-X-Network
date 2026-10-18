# select — Directory Index

## Directory Responsibility

Select component: Select, SelectItem, SelectOption type, and composed variants (chains, tokens, withOptions). SelectPrimitive for low-level usage.

## Files

| File                | Language   | Summary                                                                    | Link                                     |
| ------------------- | ---------- | -------------------------------------------------------------------------- | ---------------------------------------- |
| index.ts            | TypeScript | Re-exports Select, SelectItem, SelectOption, ChainSelectProps, SelectProps | [index.md](index.md)                     |
| select.tsx          | TSX        | Select component                                                           | [select.md](select.md)                   |
| selectPrimitive.tsx | TSX        | SelectPrimitive                                                            | [selectPrimitive.md](selectPrimitive.md) |
| chains.tsx          | TSX        | Chain select variant                                                       | [chains.md](chains.md)                   |
| tokens.tsx          | TSX        | Token select variant                                                       | [tokens.md](tokens.md)                   |
| withOptions.tsx     | TSX        | WithOptions variant                                                        | [withOptions.md](withOptions.md)         |
| combine.tsx         | TSX        | Combine select                                                             | [combine.md](combine.md)                 |

## Key Entities

| Entity          | File                   | Role                        |
| --------------- | ---------------------- | --------------------------- |
| Select          | select.tsx             | Main select component       |
| SelectItem      | select.tsx             | Select item component       |
| SelectOption    | types                  | Option type                 |
| chains / tokens | chains.tsx, tokens.tsx | Precomposed select variants |
