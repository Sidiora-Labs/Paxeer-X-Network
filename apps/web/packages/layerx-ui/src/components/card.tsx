import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../lib/utils";

const cardVariants = cva("rounded-lg bg-surface", {
  variants: {
    elevation: {
      /** Hairline border + soft shadow — the design set's default card. */
      raised: "border border-border shadow-card",
      outline: "border border-border",
      flat: "bg-surface-sunken/60",
    },
    padding: {
      none: "",
      sm: "p-3",
      md: "p-4",
      lg: "p-5",
    },
  },
  defaultVariants: { elevation: "raised", padding: "md" },
});

export interface CardProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof cardVariants> {
  asChild?: boolean;
}

const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, elevation, padding, ...props }, ref) => (
    <div ref={ref} className={cn(cardVariants({ elevation, padding, className }))} {...props} />
  ),
);
Card.displayName = "Card";

export { Card, cardVariants };
