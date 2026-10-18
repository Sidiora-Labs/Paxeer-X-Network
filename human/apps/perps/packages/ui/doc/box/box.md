# box.tsx

## box.tsx 的职责

Provides the `Box` layout primitive and `boxVariants` (tailwind-variants). Box is a polymorphic container (div by default; optional `as` or `asChild` for Slot) with layout, shadow, decoration, position, and visible variants, plus optional width, height, left, right, top, bottom, and angle for gradient. Used as the primary layout building block across the UI package.

## box.tsx 对外导出内容

| Name        | Type      | Role             | Description                        |
| ----------- | --------- | ---------------- | ---------------------------------- |
| Box         | component | Layout primitive | ForwardRef component with BoxProps |
| boxVariants | function  | tv() variants    | Style variants for box             |

## Box 的输入与输出

- **Input**: BoxProps (as, asChild, width, height, left, right, top, bottom, angle, className, and layout/shadow/decoration/position/visible variant props).
- **Output**: Rendered element (default div or Slot) with variant classes and size/position styles.

## Box Props

| Prop                     | Type                                                                                  | Required | Default | Description                              |
| ------------------------ | ------------------------------------------------------------------------------------- | -------- | ------- | ---------------------------------------- |
| asChild                  | boolean                                                                               | No       | false   | Use Radix Slot to merge props onto child |
| as                       | "div" \| "span" \| "nav" \| "section" \| "article" \| "aside" \| "header" \| "footer" | No       | "div"   | Element type when not asChild            |
| width                    | string \| number                                                                      | No       | —       | Width (e.g. "100%", 200)                 |
| height                   | string \| number                                                                      | No       | —       | Height                                   |
| left, right, top, bottom | string \| number                                                                      | No       | —       | Position values                          |
| angle                    | number                                                                                | No       | —       | Gradient angle                           |
| className                | string                                                                                | No       | —       | Additional classes                       |
| ...layoutVariants        | —                                                                                     | No       | —       | p, px, py, pt, pb, etc. from layout      |
| ...shadowVariants        | —                                                                                     | No       | —       | shadow variant                           |
| ...decorationVariants    | —                                                                                     | No       | —       | decoration variant                       |
| ...positionVariants      | —                                                                                     | No       | —       | position variant                         |
| ...visibleVariants       | —                                                                                     | No       | —       | visible variant                          |

## Box 依赖与调用关系

- **Upstream**: Slot (radix-ui), layoutVariants, decorationVariants, positionVariants, shadowVariants, visibleVariants (layout/), parseSizeProps (helpers/parse-props), tv (utils/tv).
- **Downstream**: Most layout and container components; Button and others use layout/shadow variants.

## Box Example

```tsx
import { Box } from "@paxeer-network/ui";

<Box p="4" className="flex gap-2">
  <Box width={200} height={100} shadow="md" />
  <Box as="section" px="2" py="3" />
</Box>;
```
