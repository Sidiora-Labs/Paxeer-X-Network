import * as React from "react";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { tv, cnBase } from "tailwind-variants";

const checkboxVariants = tv({
  base: [
    "peer",
    "oui-h-3",
    "oui-w-3",
    "oui-shrink-0",
    "oui-rounded-sm",
    "oui-border",
    "focus-visible:oui-outline-none",
    // "focus-visible:oui-ring-1",
    // "focus-visible:oui-ring-ring",
    "disabled:oui-cursor-not-allowed",
    "disabled:oui-opacity-50",
    // "data-[state=checked]:oui-border-none",
    // "data-[state=checked]:oui-text-base-contrast",
  ],
  variants: {
    color: {
      primary: "oui-border-primary data-[state=checked]:oui-text-base-contrast",
      white: "oui-border-base-contrast-54",
      // data-[state=checked]:oui-bg-white/80 data-[state=checked]:oui-text-[rgba(0,0,0,0.88)]
    },
    variant: {
      checkBox: "data-[state=checked]:oui-border-none",
      radio:
        "oui-rounded-full data-[state=checked]:oui-border-base-contrast-20 data-[state=checked]:oui-bg-transparent",
    },
  },
  compoundVariants: [
    {
      color: "primary",
      variant: "radio",
      className:
        "data-[state=checked]:oui-border-parimary data-[state=checked]:!oui-text-parimary",
    },
  ],
  defaultVariants: {
    color: "white",
    // style: "checkBox",
  },
});

const Checkbox = React.forwardRef<
  React.ElementRef<typeof CheckboxPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root> & {
    size?: number;
    indicatorClassName?: string;
    color?: "primary" | "white";
    variant?: "checkBox" | "radio";
  }
>(({ className, color = "white", variant = "checkBox", ...props }, ref) => {
  return (
    <CheckboxPrimitive.Root
      ref={ref}
      className={checkboxVariants({ color, className, variant })}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        className={cnBase(
          "oui-flex oui-items-center oui-justify-center oui-text-current",
          props.indicatorClassName,
        )}
      >
        {variant === "checkBox" ? (
          <CheckboxIcon
            className={cnBase(
              color === "primary"
                ? "oui-text-primary"
                : "oui-text-base-contrast",
            )}
          />
        ) : (
          <div
            className={cnBase(
              "oui-w-[6px] oui-h-[6px] oui-rounded-full",
              color === "primary"
                ? "oui-bg-primary"
                : "oui-bg-base-contrast-80",
            )}
          />
        )}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
});
Checkbox.displayName = CheckboxPrimitive.Root.displayName;

export { Checkbox };

export const CheckboxIcon: React.FC<React.SVGProps<SVGSVGElement>> = (
  props,
) => (
  <svg
    width="12"
    height="12"
    viewBox="0 0 12 12"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M3.078.71A2.333 2.333 0 0 0 .745 3.041v5.834a2.333 2.333 0 0 0 2.333 2.333H8.91a2.333 2.333 0 0 0 2.334-2.333V3.042A2.333 2.333 0 0 0 8.91.71zm5.83 2.555a.7.7 0 0 1 .488-.182c.174 0 .355.056.488.182a.643.643 0 0 1 0 .93L5.129 8.728a.723.723 0 0 1-.975 0L2.115 6.785a.644.644 0 0 1 0-.93.724.724 0 0 1 .977 0l1.55 1.476z"
      fillOpacity=".8"
    />
  </svg>
);
