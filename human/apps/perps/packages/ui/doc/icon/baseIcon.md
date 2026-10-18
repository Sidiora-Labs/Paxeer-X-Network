# baseIcon.tsx

## baseIcon.tsx 的职责

Provides `BaseIcon` component and `BaseIconProps` type. BaseIcon is an SVG wrapper with size, color variant (primary, success, danger, warning, white, black, inherit), viewBox, and standard SVG props. Used as the base for all named icons in the icon directory; exported as `Icon` from icon/index.ts with `Icon.combine` for combined icons.

## baseIcon.tsx 对外导出内容

| Name          | Type      | Role          | Description                                                                       |
| ------------- | --------- | ------------- | --------------------------------------------------------------------------------- |
| BaseIcon      | component | SVG icon base | ForwardRef SVG with BaseIconProps                                                 |
| BaseIconProps | interface | Props type    | Extends ComponentPropsWithout<"svg", RemovedProps> and VariantProps<iconVariants> |

## BaseIcon 的输入与输出

- **Input**: BaseIconProps (size, color, viewBox, opacity, className, children, fill, and SVG attributes).
- **Output**: Rendered `<svg>` with iconVariants classes and size/viewBox applied.

## BaseIcon Props

| Prop      | Type                                                                               | Required | Default     | Description             |
| --------- | ---------------------------------------------------------------------------------- | -------- | ----------- | ----------------------- |
| size      | number                                                                             | No       | 24          | Width and height of SVG |
| color     | "primary" \| "success" \| "danger" \| "warning" \| "white" \| "black" \| "inherit" | No       | "black"     | Color variant           |
| viewBox   | string                                                                             | No       | "0 0 24 24" | SVG viewBox             |
| opacity   | number                                                                             | No       | —           | SVG opacity             |
| className | string                                                                             | No       | —           | Additional classes      |
| children  | ReactNode                                                                          | No       | —           | SVG path/content        |
| fill      | string                                                                             | No       | "none"      | SVG fill                |

## BaseIcon 依赖与调用关系

- **Upstream**: iconVariants (tv), ComponentPropsWithout/RemovedProps (helpers/component-props).
- **Downstream**: All named icon components (e.g. CheckIcon, ChevronDownIcon) use BaseIcon or similar pattern; Icon = BaseIcon in icon/index.ts.

## BaseIcon Example

```tsx
import { Icon, CheckIcon } from "@paxeer-network/ui";

<Icon size={20} color="primary" />
<CheckIcon size={24} color="success" />
```
