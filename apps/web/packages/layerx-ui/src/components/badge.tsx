import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-full font-semibold whitespace-nowrap [&_svg]:size-3",
  {
    variants: {
      variant: {
        /** Default gray pill — Active/Inactive, Settled/Pending. */
        neutral: "bg-surface-sunken text-foreground-secondary",
        success: "bg-success-soft text-success",
        destructive: "bg-destructive-soft text-destructive",
        warning: "bg-warning-soft text-warning",
        accent: "bg-accent-soft text-accent-strong",
        outline: "border border-border-strong text-foreground-secondary",
      },
      size: {
        sm: "h-6 px-2.5 text-xs",
        md: "h-7 px-3 text-[13px]",
      },
    },
    defaultVariants: { variant: "neutral", size: "md" },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, size, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, size, className }))} {...props} />;
}

export { badgeVariants };
