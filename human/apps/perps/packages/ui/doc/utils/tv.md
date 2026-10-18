# tv.ts

## tv.ts 的职责

Exports `tv` — a wrapper around `createTV` from tailwind-variants with `twMergeConfig.prefix: "oui-"`. Used by Box, Button, and most UI components to build variant-aware class names with the paxeer-network UI prefix. Main package re-exports tv from utils.

## tv.ts 对外导出内容

| Name | Type     | Role                      | Description                                                  |
| ---- | -------- | ------------------------- | ------------------------------------------------------------ |
| tv   | function | createTV with oui- prefix | Returns variant composer with tailwind-merge and oui- prefix |

## tv 的输入与输出

- **Input**: Config object passed to createTV (base, variants, compoundVariants, slots, etc.).
- **Output**: Function that returns class string from variant props and className.

## tv 依赖与调用关系

- **Upstream**: tailwind-variants (createTV).
- **Downstream**: box, button, icon, avatar, and most components under src.

## tv Example

```ts
import { tv } from "@paxeer-network/ui";

const variants = tv({
  base: "oui-px-2 oui-py-1",
  variants: { size: { sm: "oui-text-sm", md: "oui-text-base" } },
  defaultVariants: { size: "md" },
});
variants({ size: "sm", className: "extra" }); // "oui-px-2 oui-py-1 oui-text-sm extra"
```
