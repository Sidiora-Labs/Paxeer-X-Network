import * as React from "react";
import { Tooltip as TooltipPrimitive } from "@base-ui/react/tooltip";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

function TooltipProvider({ delay = 300, ...props }: TooltipPrimitive.Provider.Props) {
  return <TooltipPrimitive.Provider data-slot="tooltip-provider" delay={delay} {...props} />;
}

function Tooltip(props: TooltipPrimitive.Root.Props) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />;
}

function TooltipTrigger(props: TooltipPrimitive.Trigger.Props) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />;
}

const tooltipVariants = cva(
  "popup-motion w-fit max-w-xs px-2.5 py-1.5 text-xs font-medium text-balance data-instant:transition-none",
  {
    variants: {
      variant: {
        inverted: "rounded-sm bg-foreground text-background",
        outline: "rounded-md bg-popover text-popover-foreground outline-1 outline-border shadow-sm",
      },
    },
    defaultVariants: { variant: "inverted" },
  },
);

/** Bordered arrow. Points down; rotated per side. */
function TooltipArrowSvg({ outline }: { outline: boolean }) {
  const fill = outline ? "var(--color-popover)" : "var(--color-foreground)";
  return (
    <svg width="20" height="10" viewBox="0 0 20 10" fill="none" aria-hidden>
      <path
        d="M10.3356 7.39793L15.1924 3.02682C15.9269 2.36577 16.8801 2 17.8683 2H20V0H0V2H1.4651C2.4532 2 3.4064 2.36577 4.1409 3.02682L8.9977 7.39793C9.378 7.7402 9.9553 7.74021 10.3356 7.39793Z"
        fill={fill}
      />
      {outline && (
        <path
          d="M9.6667 6.65461L14.5235 2.28352C15.4416 1.45721 16.6331 1 17.8683 1H20V2H17.8683C16.8801 2 15.9269 2.36577 15.1924 3.02682L10.3356 7.39793C9.9553 7.74021 9.378 7.7402 8.9977 7.39793L4.1409 3.02682C3.4064 2.36577 2.4532 2 1.4651 2H0V1H1.4651C2.7002 1 3.8917 1.45722 4.8099 2.28352L9.6667 6.65461Z"
          fill="var(--color-border)"
        />
      )}
    </svg>
  );
}

function TooltipContent({
  className,
  side = "top",
  sideOffset,
  align = "center",
  variant = "inverted",
  arrow = false,
  children,
  ...props
}: TooltipPrimitive.Popup.Props &
  Pick<TooltipPrimitive.Positioner.Props, "side" | "sideOffset" | "align"> &
  VariantProps<typeof tooltipVariants> & {
    /** Render a pointer arrow. */
    arrow?: boolean;
  }) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Positioner className="z-50" side={side} sideOffset={sideOffset ?? (arrow ? 10 : 6)} align={align}>
        <TooltipPrimitive.Popup data-slot="tooltip-content" className={cn(tooltipVariants({ variant }), className)} {...props}>
          {children}
          {arrow && (
            <TooltipPrimitive.Arrow
              data-slot="tooltip-arrow"
              className={cn(
                "flex",
                "data-[side=top]:-bottom-[9px]",
                "data-[side=bottom]:-top-[9px] data-[side=bottom]:rotate-180",
                "data-[side=left]:-right-[14px] data-[side=left]:-rotate-90",
                "data-[side=right]:-left-[14px] data-[side=right]:rotate-90",
              )}
            >
              <TooltipArrowSvg outline={variant === "outline"} />
            </TooltipPrimitive.Arrow>
          )}
        </TooltipPrimitive.Popup>
      </TooltipPrimitive.Positioner>
    </TooltipPrimitive.Portal>
  );
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider, tooltipVariants };
export type TooltipContentProps = React.ComponentProps<typeof TooltipContent>;
