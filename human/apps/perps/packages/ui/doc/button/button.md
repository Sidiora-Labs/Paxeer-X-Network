# button.tsx

## button.tsx 的职责

Provides the main `Button` component and `buttonVariants` (tailwind-variants) for paxeer-network UI. Button supports variant (text, outlined, contained, gradient), size (xs–xl), color (primary, secondary, success, buy, sell, danger, warning, gray, light), fullWidth, shadow, and angle for gradient. Renders via BaseButton with compound variant styles.

## button.tsx 对外导出内容

| Name           | Type      | Role                  | Description                                                |
| -------------- | --------- | --------------------- | ---------------------------------------------------------- |
| Button         | component | Main button component | ForwardRef button with ButtonProps                         |
| buttonVariants | function  | tv() variants         | Style variants for button                                  |
| ButtonProps    | type      | Props type            | Extends BaseButtonProps and VariantProps of buttonVariants |

## Button 的输入与输出

- **Input**: Props (children, variant, size, color, fullWidth, shadow, angle, className, style, and rest button attributes).
- **Output**: Rendered `<button>` (via BaseButton) with variant-based classes and optional angle style.

## Button Props

| Prop      | Type                                                                                                   | Required | Default     | Description                                |
| --------- | ------------------------------------------------------------------------------------------------------ | -------- | ----------- | ------------------------------------------ |
| variant   | "text" \| "outlined" \| "contained" \| "gradient"                                                      | No       | "contained" | Visual variant                             |
| size      | "xs" \| "sm" \| "md" \| "lg" \| "xl"                                                                   | No       | "lg"        | Button size (height/padding)               |
| color     | "primary" \| "secondary" \| "success" \| "buy" \| "sell" \| "danger" \| "warning" \| "gray" \| "light" | No       | "primary"   | Color theme                                |
| fullWidth | boolean                                                                                                | No       | false       | Stretch to full width                      |
| shadow    | VariantProps<shadowVariants>                                                                           | No       | —           | Shadow variant from layout/shadow          |
| angle     | number                                                                                                 | No       | —           | Gradient angle (passed to parseAngleProps) |
| className | string                                                                                                 | No       | —           | Additional class names                     |
| style     | React.CSSProperties                                                                                    | No       | —           | Inline styles (merged with angle style)    |
| ...rest   | ButtonHTMLAttributes                                                                                   | —        | —           | Native button props                        |

## Button 依赖与调用关系

- **Upstream**: BaseButton (./base), buttonVariants (tv + shadowVariants from layout/shadow), parseAngleProps (helpers/parse-props), SizeType (helpers/sizeType).
- **Downstream**: Applications; IconButton and other button-like components may reuse BaseButton.

## Button 的渲染与状态流程

1. Props (variant, size, color, fullWidth, shadow, angle, style, …) are received.
2. parseAngleProps({ angle }) produces optional angle-related style.
3. buttonVariants({ variant, size, color, className, fullWidth, shadow }) produces class string.
4. BaseButton renders with combined className, size, ref, style (style + angleStyle), and rest props.

## Button 的错误与边界

| Scenario              | Condition     | Behavior                           | Handling                       |
| --------------------- | ------------- | ---------------------------------- | ------------------------------ |
| Disabled              | disabled=true | Cursor and colors from base styles | Handled in buttonVariants base |
| Unknown variant/color | —             | defaultVariants apply              | Safe fallback                  |

## Button Example

```tsx
import { Button } from "@paxeer-network/ui";

<Button variant="contained" color="primary" size="lg">Submit</Button>
<Button variant="outlined" color="danger" size="sm">Cancel</Button>
<Button variant="text" color="primary">Link style</Button>
<Button variant="gradient" fullWidth>Gradient CTA</Button>
```
