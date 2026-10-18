import React from "react";
import { Slot } from "@radix-ui/react-slot";
import { VariantProps } from "tailwind-variants";
import { parseSizeProps } from "../helpers/parse-props";
import { decorationVariants } from "../layout/decoration";
import { layoutVariants } from "../layout/layout";
import { positionVariants } from "../layout/position";
import { shadowVariants } from "../layout/shadow";
import { visibleVariants } from "../layout/visible";
import { tv } from "../utils/tv";

const boxVariants = tv({
  extend: shadowVariants,
  base: ["oui-box"],
  variants: {
    ...layoutVariants.variants,
    ...decorationVariants.variants,
    ...positionVariants.variants,
    ...visibleVariants.variants,
    __position: {
      true: "oui-position",
    },
    __size_width: {
      true: "oui-size-width",
    },
    __size_height: {
      true: "oui-size-height",
    },
  },
  defaultVariants: {
    __position: false,
    __size: false,
  },
});

// // @ts-ignore
// const boxVariants = tv({
//   extend: baseBoxVariants,
// });

type BoxElement = React.ElementRef<"div">;

interface BoxProps
  extends
    React.ButtonHTMLAttributes<HTMLDivElement | HTMLSpanElement>,
    Omit<
      VariantProps<typeof boxVariants>,
      "__position" | "__size_width" | "__size_height"
    > {
  asChild?: boolean;
  as?:
    | "div"
    | "span"
    | "nav"
    | "section"
    | "article"
    | "aside"
    | "header"
    | "footer";
  width?: string | number;
  height?: string | number;
  left?: string | number;
  right?: string | number;
  top?: string | number;
  bottom?: string | number;
  /**
   * Angle of the gradient
   */
  angle?: number;
}

const Box = React.forwardRef<BoxElement, BoxProps>((props, forwardedRef) => {
  const {
    asChild = false,
    as: TAG = "div",
    className,
    p,
    px,
    py,
    pt,
    pb,
    pl,
    pr,
    m,
    mx,
    my,
    mr,
    mt,
    mb,
    ml,
    grow,
    zIndex,
    style,
    shadow,
    border,
    gradient,
    r,
    invisible,
    intensity,
    position,
    borderColor,
    children,
    ...rest
  } = parseSizeProps(props);

  const Comp = asChild ? Slot : TAG;

  return (
    <Comp
      style={style}
      className={boxVariants({
        className,
        p,
        r,
        px,
        py,
        pt,
        pb,
        pr,
        pl,
        m,
        mx,
        my,
        mt,
        mb,
        ml,
        mr,
        zIndex,
        shadow,
        border,
        gradient,
        position,
        intensity,
        invisible,
        grow,
        borderColor,
        __position: typeof position !== "undefined",
        __size_width: typeof props.width !== "undefined",
        __size_height: typeof props.height !== "undefined",
      })}
      {...rest}
      ref={forwardedRef}
    >
      {children}
    </Comp>
  );
});

Box.displayName = "Box";

export { Box, boxVariants };

export type { BoxProps };
