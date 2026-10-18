import React from "react";
import { VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";

const dividerVariants = tv({
  base: ["oui-pointer-events-none oui-box-content"],
  variants: {
    intensity: {
      4: "oui-border-line-4",
      6: "oui-border-line-6",
      8: "oui-border-line",
      12: "oui-border-line-12",
      16: "oui-border-line-16",
    },
    direction: {
      horizontal: ["oui-border-b"],
      vertical: ["oui-border-s"],
    },
    lineStyle: {
      // solid: "oui-border-solid",
      dashed: "oui-border-dashed",
      dotted: "oui-border-dotted",
    },
    mx: {
      2: "oui-mx-2",
      4: "oui-mx-4",
      6: "oui-mx-6",
      8: "oui-mx-8",
      10: "oui-mx-10",
    },
    my: {
      2: "oui-my-2",
      4: "oui-my-4",
      6: "oui-my-6",
      8: "oui-my-8",
      10: "oui-my-10",
    },
  },
  defaultVariants: {
    direction: "horizontal",
    intensity: 4,
  },
});

type DividerProps = VariantProps<typeof dividerVariants> &
  React.HTMLAttributes<HTMLDivElement>;

const Divider = React.forwardRef<HTMLDivElement, DividerProps>((props, ref) => {
  const { className, intensity, direction, lineStyle, mx, my, ...rest } = props;
  return (
    <div
      ref={ref}
      {...rest}
      className={dividerVariants({
        direction,
        intensity,
        className,
        lineStyle,
        mx,
        my,
      })}
    />
  );
});

Divider.displayName = "Divider";

export { Divider, dividerVariants };
export type { DividerProps };
