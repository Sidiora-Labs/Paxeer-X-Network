import { VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";
import {
  ComponentPropsWithout,
  RemovedProps,
} from "../helpers/component-props";

const badgeVariants = tv({
  base: "oui-inline-flex oui-items-center oui-rounded-md oui-font-semibold oui-transition-colors focus:oui-outline-none focus:oui-ring-2 focus:oui-ring-ring focus:oui-ring-offset-2",

  variants: {
    variant: {
      contained: "",
      // outlined: "oui-border",
      text: "",
    },
    color: {
      primary: "",
      primaryLight: "",
      secondary: "",
      danger: "",
      buy: "",
      success: "",
      warning: "",
      sell: "",
      neutral: "",
    },
    size: {
      xs: "oui-px-2 oui-py-0.5 oui-text-2xs oui-rounded oui-h-[18px]",
      sm: "oui-px-2 oui-py-0.5 oui-text-2xs oui-rounded",
      md: "oui-px-2 oui-py-0.5 oui-text-sm",
      lg: "oui-px-2 oui-py-1 oui-text-base",
      // xl: "oui-px-4 oui-py-1.5 oui-text-sm",
    },
    // dot:{
    //   true: "oui-w-2 oui-h-2 oui-rounded-full",
    //   false: ""
    // }
  },
  compoundVariants: [
    {
      variant: "contained",
      color: "primary",
      className: ["oui-bg-primary/15", "oui-text-primary"],
    },
    {
      variant: "contained",
      color: "primaryLight",
      className: ["oui-bg-primary-light/15", "oui-text-primary-light"],
    },
    {
      variant: "contained",
      color: "danger",
      className: ["oui-bg-danger/15", "oui-text-danger"],
    },
    {
      variant: "contained",
      color: "sell",
      className: ["oui-bg-danger/15", "oui-text-danger"],
    },
    {
      variant: "contained",
      color: "success",
      className: ["oui-bg-success/15", "oui-text-success"],
    },
    {
      variant: "contained",
      color: "buy",
      className: ["oui-bg-success/15", "oui-text-success"],
    },
    {
      variant: "contained",
      color: "warning",
      className: ["oui-bg-warning-darken/15", "oui-text-warning-darken"],
    },
    {
      variant: "contained",
      color: "neutral",
      className: ["oui-bg-line", "oui-text-base-contrast-36"],
    },
    {
      variant: "text",
      color: "primary",
      className: ["oui-text-primary"],
    },
    {
      variant: "text",
      color: "danger",
      className: ["oui-text-danger"],
    },
    {
      variant: "text",
      color: "success",
      className: ["oui-text-success"],
    },
    {
      variant: "text",
      color: "warning",
      className: ["oui-text-warning-darken"],
    },
    {
      variant: "text",
      color: "neutral",
      className: ["oui-text-base-contrast-36"],
    },
  ],
  defaultVariants: {
    variant: "contained",
    color: "primary",
    size: "md",
  },
});

export interface BadgeProps
  extends ComponentPropsWithout<"div", RemovedProps>,
    VariantProps<typeof badgeVariants> {
  // dot?: boolean;
}

function Badge({ className, variant, color, size, ...props }: BadgeProps) {
  return (
    <div
      className={badgeVariants({ variant, className, color, size })}
      {...props}
    />
  );
}

export { Badge, badgeVariants };
