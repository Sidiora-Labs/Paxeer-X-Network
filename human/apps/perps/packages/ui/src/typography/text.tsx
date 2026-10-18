import React, { MouseEventHandler } from "react";
import { Slot } from "@radix-ui/react-slot";
import { type VariantProps } from "tailwind-variants";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";
import { tv } from "../utils/tv";

const textVariants = tv({
  variants: {
    size: {
      "3xs": ["oui-text-3xs"],
      "2xs": ["oui-text-2xs"],
      xs: ["oui-text-xs"],
      sm: ["oui-text-sm"],
      base: ["oui-text-base"],
      lg: ["oui-text-lg"],
      xl: ["oui-text-xl"],
      "2xl": ["oui-text-2xl"],
      "3xl": ["oui-text-3xl"],
      "4xl": ["oui-text-4xl"],
      "5xl": ["oui-text-5xl"],
      "6xl": ["oui-text-6xl"],
    },
    weight: {
      regular: ["oui-font-normal"],
      semibold: ["oui-font-semibold"],
      bold: ["oui-font-bold"],
    },
    color: {
      inherit: "oui-text-inherit",
      neutral: "oui-text-base-contrast-54",
      primary: "oui-text-primary",
      primaryLight: "oui-text-primary-light",
      secondary: "oui-text-secondary",
      tertiary: "oui-text-tertiary",
      // quaternary: "oui-text-gray-300",
      warning: "oui-text-warning-darken",
      danger: "oui-text-danger",
      success: "oui-text-success",
      buy: "oui-text-trade-profit",
      sell: "oui-text-trade-loss",
      lose: "oui-text-trade-loss",
      withdraw: "oui-text-trade-loss",
      profit: "oui-text-trade-profit",
      deposit: "oui-text-trade-profit",
      // gradient
    },
    intensity: {
      12: "oui-text-base-contrast-12",
      20: "oui-text-base-contrast-20",
      36: "oui-text-base-contrast-36",
      54: "oui-text-base-contrast-54",
      80: "oui-text-base-contrast-80",
      98: "oui-text-base-contrast",
    },
    dashBoard: {
      default: "oui-border-b oui-border-dashed oui-border-base-contrast-12",
    },
  },
});

export type TextElement = React.ElementRef<"span">;

interface BasicTextProps extends VariantProps<typeof textVariants> {
  asChild?: boolean;
}

interface CopyableTextProps extends BasicTextProps {
  /**
   * If true, the text will be copied when clicked.
   */
  copyable?: boolean;
  /**
   * Callback when the text is copied.
   */
  onCopy?: (event: React.MouseEvent<HTMLButtonElement, MouseEvent>) => void;
  /** default is 12 */
  copyIconSize?: number;
  copyIconTestid?: string;
}

type TextSpanProps = { as?: "span" } & ComponentPropsWithout<
  "span",
  RemovedProps
>;
type TextDivProps = { as: "div" } & ComponentPropsWithout<"div", RemovedProps>;
type TextLabelProps = { as: "label" } & ComponentPropsWithout<
  "label",
  RemovedProps
>;
type TextPProps = { as: "p" } & ComponentPropsWithout<"p", RemovedProps>;
type TextProps = BasicTextProps &
  (TextSpanProps | TextDivProps | TextLabelProps | TextPProps);

const Text = React.forwardRef<TextElement, TextProps>((props, forwardedRef) => {
  const {
    children,
    className,
    asChild,
    as: Tag = "span",
    color,
    size,
    weight,
    intensity,
    ...textProps
  } = props;
  return (
    <Slot
      data-accent-color={color}
      {...textProps}
      ref={forwardedRef}
      className={textVariants({
        className,
        color,
        size,
        weight,
        intensity,
      })}
    >
      {asChild ? children : <Tag>{children}</Tag>}
    </Slot>
  );
});
Text.displayName = "Text";

export { Text, textVariants };
export type { TextProps, CopyableTextProps };
