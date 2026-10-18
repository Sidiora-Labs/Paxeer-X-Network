import React, { CSSProperties } from "react";
import { VariantProps } from "tailwind-variants";
import { parseAngleProps } from "../helpers/parse-props";
import { tv } from "../utils/tv";
import { textVariants, type TextProps, Text } from "./text";

const gradientTextVariants = tv({
  //   extend: textVariants,
  base: "oui-text-transparent oui-bg-clip-text",
  variants: {
    color: {
      primary: "oui-gradient-primary",
      brand: "oui-gradient-brand",
      success: "oui-gradient-success",
      warning: "oui-gradient-warning",
      danger: "oui-gradient-danger",
      neutral: "oui-gradient-neutral",
      //   transparent: "oui-gradient-transparent",
      inherit: "oui-text-inherit",
    },
  },
});

type GradientTextProps = Omit<TextProps, "color"> &
  VariantProps<typeof gradientTextVariants> & {
    /**
     * Angle of the gradient
     */
    angle?: number;
  };

const GradientText = React.forwardRef<
  React.ElementRef<"span">,
  GradientTextProps
>((props, ref) => {
  const { color, className, angle, ...rest } = props;
  const style = parseAngleProps({ angle });
  return (
    // @ts-ignore
    <Text
      {...rest}
      ref={ref}
      className={gradientTextVariants({ color, className })}
      style={style as CSSProperties}
    />
  );
});

export { gradientTextVariants, GradientText };

GradientText.displayName = "GradientText";
