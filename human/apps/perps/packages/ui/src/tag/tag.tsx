import { Slot } from "@radix-ui/react-slot";
import React, { HTMLAttributes } from "react";
import { tv, type VariantProps } from "tailwind-variants";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";

const tagVariants = tv({
  base: "tag",
  variants: {
    color: {
      primary: "oui-text-primary-darken",
      //   secondary: "tag-secondary",
      success: "oui-text-success",
      warning: "oui-text-warning-darken",
      danger: "oui-text-danger",
      //   info: "tag-info",
    },
    // size: {
    //   sm: "tag-sm",
    //   md: "tag-md",
    //   lg: "tag-lg",
    // },
  },
});

interface TagProps
  extends ComponentPropsWithout<"div", RemovedProps>,
    VariantProps<typeof tagVariants> {
  asChild?: boolean;
  tag?: "span" | "div";
}

const Tag = React.forwardRef<HTMLDivElement, TagProps>((props, ref) => {
  const { asChild, className, tag = "span", ...rest } = props;
  const Comp = asChild ? Slot : tag;
  return <Comp ref={ref} className={className} {...rest} />;
});

export { Tag, tagVariants };
export type { TagProps };
