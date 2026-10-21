import * as React from "react"

import { cn } from "@/ui/utils/cn"

export interface InputProps
  extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, label, ...props }, ref) => {
    return (
      <div className="relative flex w-full bg-surface-2 rounded-[var(--radius-md)] h-9">
        {label && (
          <span className="absolute text-sm -translate-y-1/2 left-3 top-1/2 text-muted-foreground">
            {label}
          </span>
        )}
        <input
          type={type}
          className={cn(
            "w-full bg-transparent px-3 py-1 text-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/30 disabled:cursor-not-allowed disabled:opacity-50 rounded-[var(--radius-md)]",
            label && "text-right pr-3",
            className
          )}
          ref={ref}
          {...props}
        />
      </div>
    )
  }
)
Input.displayName = "Input"

export { Input }
