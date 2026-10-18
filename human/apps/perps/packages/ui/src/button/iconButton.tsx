import React from "react";
import { tv, type VariantProps } from "tailwind-variants";
import { parseAngleProps } from "../helpers/parse-props";
import { BaseButton, BaseButtonProps } from "./base";

const iconButtonVariants = tv({
  base: [
    "oui-button",
    "oui-inline-flex",
    "oui-items-center",
    "oui-justify-center",
    "oui-transition-colors",
    "disabled:oui-cursor-not-allowed",
    "disabled:oui-opacity-36",
  ],
  variants: {
    color: {
      primary: [
        "oui-text-primary-darken",
        "hover:oui-text-primary-darken/80",
        "active:oui-text-primary-darken/70",
      ],
      secondary: [
        "oui-text-base-contrast-54",
        "hover:oui-text-base-contrast-80",
        "active:oui-text-base-contrast-70",
      ],
      success: [
        "oui-text-success",
        "hover:oui-text-success/80",
        "active:oui-text-success/70",
      ],
      buy: [
        "oui-text-success",
        "hover:oui-text-success/80",
        "active:oui-text-success/70",
      ],
      danger: [
        "oui-text-danger",
        "hover:oui-text-danger/80",
        "active:oui-text-danger/70",
      ],
      sell: [
        "oui-text-danger",
        "hover:oui-text-danger/80",
        "active:oui-text-danger/70",
      ],
      warning: [
        "oui-text-warning-darken",
        "hover:oui-text-warning-darken/80",
        "active:oui-text-warning-darken/70",
      ],
      gray: [
        "oui-text-base",
        "hover:oui-text-base/80",
        "active:oui-text-base/70",
      ],
      light: [
        "oui-text-white",
        "hover:oui-text-white/80",
        "active:oui-text-white/70",
      ],
    },
  },
  defaultVariants: {
    color: "secondary",
  },
});

interface IconButtonProps
  extends Omit<BaseButtonProps, "size" | "children">,
    VariantProps<typeof iconButtonVariants> {
  children: React.ReactElement;
  angle?: number;
  "data-testid"?: string;
}

const IconButton = React.forwardRef<HTMLButtonElement, IconButtonProps>(
  ({ className, color, angle, style, children, ...props }, ref) => {
    const angleStyle = parseAngleProps({ angle });
    // BaseButton requires size, but we don't apply size styles
    // Pass a default size that won't affect the icon size
    return (
      <BaseButton
        className={iconButtonVariants({
          color,
          className,
        })}
        size="md"
        ref={ref}
        style={{ ...style, ...angleStyle }}
        {...props}
      >
        {children}
      </BaseButton>
    );
  },
);
IconButton.displayName = "IconButton";

export { IconButton, iconButtonVariants };

export type { IconButtonProps };
