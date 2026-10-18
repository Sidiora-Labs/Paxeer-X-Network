import React, { PropsWithChildren } from "react";
import { tv, type VariantProps } from "tailwind-variants";
import { parseAngleProps } from "../helpers/parse-props";
import { SizeType } from "../helpers/sizeType";
import { shadowVariants } from "../layout/shadow";
import { BaseButton, BaseButtonProps } from "./base";

const buttonVariants = tv(
  {
    extend: shadowVariants,
    base: [
      "oui-button",
      "oui-inline-flex",
      "oui-items-center",
      "oui-justify-center",
      "oui-whitespace-nowrap",
      "oui-transition-colors",
      "disabled:oui-cursor-not-allowed",
      "disabled:oui-bg-base-3",
      "disabled:oui-text-base-contrast-36",
      "disabled:hover:oui-bg-base-3",
    ],
    variants: {
      variant: {
        text: "oui-bg-transparent",
        outlined: "oui-border",
        contained: "oui-text-base-contrast",
        // Token-driven brand gradient: real gradient in themes that define
        // gradient values, flat brand fill under flattened presets (ODS).
        gradient: "oui-gradient-brand oui-text-base-10",
      },
      size: {
        xs: ["oui-px-2", "oui-rounded", "oui-h-6", "oui-text-2xs"], //24px
        sm: ["oui-px-3", "oui-rounded", "oui-h-7", "oui-text-2xs"], //28px
        md: ["oui-px-3", "oui-rounded-md", "oui-h-8", "oui-text-sm"], //32px
        lg: ["oui-px-3", "oui-rounded-md", "oui-h-10", "oui-text-base"], //40px
        xl: ["oui-px-4", "oui-rounded-lg", "oui-h-13", "oui-text-lg"], //54px
      },
      color: {
        primary: [
          "hover:oui-bg-primary-darken/80 active:oui-bg-primary-darken/70",
        ],
        secondary: "hover:oui-bg-base-4/80 active:oui-bg-base-4/70",
        success: "hover:oui-bg-success/80 active:oui-bg-success/70",
        buy: "hover:oui-bg-success/80 active:oui-bg-success/70",
        danger: "hover:oui-bg-danger/80 active:oui-bg-danger/70",
        sell: "hover:oui-bg-danger/80 active:oui-bg-danger/70",
        warning:
          "hover:oui-bg-warning-darken/80 active:oui-bg-warning-darken/70",
        gray: "hover:oui-bg-base-2/80 active:oui-bg-base-2/70",
        light:
          "hover:oui-bg-base-static-80 active:oui-bg-base-static-54 disable:oui-bg-base-static-20",
      },
      fullWidth: {
        true: "oui-w-full",
      },
      // disabled: {
      //   true: "oui-bg-base-3 oui-text-base-contrast-36",
      // },
    },
    compoundVariants: [
      // contained
      {
        variant: "contained",
        color: "primary",
        className: ["oui-bg-primary-darken", "oui-text-primary-contrast"],
      },
      {
        variant: "contained",
        color: "secondary",
        className: ["oui-bg-base-4"],
      },
      {
        variant: "contained",
        color: "success",
        className: ["oui-bg-success", "oui-text-success-contrast"],
      },
      {
        variant: "contained",
        color: "buy",
        className: ["oui-bg-trade-profit", "oui-text-success-contrast"],
      },
      {
        variant: "contained",
        color: "warning",
        className: ["oui-bg-warning-darken", "oui-text-warning-contrast"],
      },
      {
        variant: "contained",
        color: "danger",
        className: ["oui-bg-danger", "oui-text-danger-contrast"],
      },
      {
        variant: "contained",
        color: "sell",
        className: ["oui-bg-trade-loss", "oui-text-danger-contrast"],
      },
      {
        variant: "contained",
        color: "gray",
        className: ["oui-bg-base-2", "oui-text-base-contrast"],
      },
      {
        variant: "contained",
        color: "light",
        className: [
          "oui-bg-base-static",
          "oui-text-base-static-contrast-80 disabled:oui-text-base-static-contrast-36",
          "disabled:oui-bg-base-static-36 hover:disabled:oui-bg-base-static-36",
        ],
      },

      {
        variant: "outlined",
        color: "primary",
        className: [
          "oui-border-primary-darken",
          "oui-text-primary-darken",
          "hover:oui-bg-primary-darken/20",
        ],
      },
      {
        variant: "outlined",
        color: "secondary",
        className: [
          "oui-border-base-4",
          "oui-text-base-contrast-54",
          "hover:oui-bg-base-4/20",
        ],
      },
      {
        variant: "outlined",
        color: "success",
        className: [
          "oui-border-success",
          "oui-text-success",
          "hover:oui-bg-success/20",
        ],
      },
      {
        variant: "outlined",
        color: "warning",
        className: [
          "oui-border-warning-darken",
          "oui-text-warning-contrast",
          "hover:oui-bg-warning-darken/20",
        ],
      },
      {
        variant: "outlined",
        color: "danger",
        className: [
          "oui-border-danger",
          "oui-text-danger",
          "hover:oui-bg-danger/20",
        ],
      },
      {
        variant: "outlined",
        color: "gray",
        className: ["oui-border-base-2"],
      },
      // outlined end
      // {
      //   variant: "outlined",
      //   color: "darkGray",
      //   className: ["oui-border-base-4", "oui-text-base"],
      // },
      // text
      {
        variant: "text",
        color: "primary",
        className: ["oui-text-primary-darken hover:oui-bg-primary-darken/10"],
      },
      {
        variant: "text",
        color: "success",
        className: ["oui-text-success hover:oui-bg-success/10"],
      },
      {
        variant: "text",
        color: "warning",
        className: ["oui-text-warning-contrast hover:oui-bg-warning-darken/10"],
      },
      {
        variant: "text",
        color: "danger",
        className: ["oui-text-danger hover:oui-bg-danger/10"],
      },
      {
        variant: "text",
        color: "gray",
        className: ["hover:oui-bg-base-2/10"],
      },
      {
        variant: "text",
        color: "secondary",
        className: ["oui-text-base-contrast-36 hover:oui-bg-base-2/10"],
      },
    ],
    defaultVariants: {
      size: "lg",
      variant: "contained",
      color: "primary",
      // elevation: "none",
    },
  },
  {
    responsiveVariants: ["md", "lg"],
  },
);

interface ButtonProps
  extends Omit<BaseButtonProps, "size">, VariantProps<typeof buttonVariants> {
  angle?: number;
  "data-testid"?: string;
}

const Button = React.forwardRef<
  HTMLButtonElement,
  PropsWithChildren<ButtonProps>
>(
  (
    {
      className,
      variant,
      size,
      color,
      fullWidth,
      shadow,
      angle,
      style,
      ...props
    },
    ref,
  ) => {
    // const Comp = asChild ? Slot : "button";
    const angleStyle = parseAngleProps({ angle });
    return (
      <BaseButton
        className={buttonVariants({
          variant,
          size,
          color,
          className,
          fullWidth,
          shadow,
        })}
        size={size as SizeType}
        ref={ref}
        style={{ ...style, ...angleStyle }}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };

export type { ButtonProps };
