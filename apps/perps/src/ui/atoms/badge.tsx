import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/ui/utils/cn"

const badgeVariants = cva(
  "inline-flex items-center rounded-[var(--radius-pill)] px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none",
  {
    variants: {
      variant: {
        default:
          "bg-primary/15 text-primary",
        secondary:
          "bg-surface-2 text-secondary-foreground",
        destructive:
          "bg-destructive/15 text-destructive",
        outline: "bg-surface-1 text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants } 