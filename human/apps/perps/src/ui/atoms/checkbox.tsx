"use client"

import * as React from "react"
import { cn } from "@/ui/utils/cn"

interface CheckboxProps extends React.InputHTMLAttributes<HTMLInputElement> {
  className?: string
}

const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
  ({ className, ...props }, ref) => (
    <input
      type="checkbox"
      className={cn(
        "h-4 w-4 rounded border-primary text-primary focus:ring-primary",
        className
      )}
      ref={ref}
      {...props}
    />
  )
)
Checkbox.displayName = "Checkbox"

export { Checkbox } 