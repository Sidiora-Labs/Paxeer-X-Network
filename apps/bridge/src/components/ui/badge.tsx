import * as React from "react";
import { useRender } from "@base-ui/react/use-render";
import { mergeProps } from "@base-ui/react/merge-props";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap transition-colors [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "bg-primary primary-fill text-primary-foreground",
        tonal: "bg-container-high text-foreground",
        outline: "border border-border text-foreground",
        destructive: "bg-destructive-container text-destructive",
        success: "bg-success-container text-success",
        warning: "bg-warning-container text-warning",
        info: "bg-info-container text-info",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

type BadgeProps = useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants>;

function Badge({ className, variant, render, ...props }: BadgeProps) {
  return useRender({
    defaultTagName: "span",
    render,
    props: mergeProps<"span">(
      { className: cn(badgeVariants({ variant }), className), "data-slot": "badge" } as React.ComponentProps<"span">,
      props,
    ),
  });
}

export { Badge, badgeVariants };
