import { Slot } from "@radix-ui/react-slot";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";
import React from "react";
import { tv, type VariantProps } from "tailwind-variants";

const inputHelpTextVariants = tv({
  base: [
    "oui-text-xs",
    "oui-text-base-contrast-36",
    "oui-mt-1",
    "oui-list-disc",
    "oui-list-inside",
    "oui-list-item",
  ],
  variants: {
    color: {
      success: ["oui-text-success"],
      danger: ["oui-text-danger"],
      warning: ["oui-text-warning-darken"],
      default: ["oui-text-base-contrast-54"],
    },
  },
});

export type InputHelpTextVariantProps = VariantProps<
  typeof inputHelpTextVariants
>;

interface InputHelpTextProps
  extends ComponentPropsWithout<"div", RemovedProps>,
    InputHelpTextVariantProps {
  asChild?: boolean;
}

const InputHelpText = React.forwardRef<HTMLDivElement, InputHelpTextProps>(
  (props, ref) => {
    const { className, asChild, color, ...rest } = props;
    const Comp = asChild ? Slot : "div";
    return (
      <Comp
        ref={ref}
        className={inputHelpTextVariants({
          className,
          color,
        })}
        {...rest}
      />
    );
  }
);

InputHelpText.displayName = "InputHelpText";

export { InputHelpText, inputHelpTextVariants };

export { type InputHelpTextProps };
