import * as React from "react";
import { motion } from "framer-motion";
import { cn } from "@/ui/utils/cn";

export interface SegmentedControlOption {
  value: string;
  label: React.ReactNode;
}

export interface SegmentedControlProps {
  options: SegmentedControlOption[];
  value: string;
  onValueChange: (value: string) => void;
  className?: string;
  size?: "sm" | "md" | "lg";
}

export function SegmentedControl({
  options,
  value,
  onValueChange,
  className,
  size = "md",
}: SegmentedControlProps) {
  const sizeClasses = {
    sm: "h-7 text-xs",
    md: "h-8 text-sm",
    lg: "h-10 text-sm",
  };

  const paddingClasses = {
    sm: "px-2.5 py-1",
    md: "px-3 py-1.5",
    lg: "px-4 py-2",
  };

  return (
    <div
      className={cn(
        "inline-flex items-center gap-0.5 rounded-[var(--radius-lg)] bg-[hsl(var(--surface-1))] p-1",
        sizeClasses[size],
        className
      )}
    >
      {options.map((option) => {
        const isActive = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => onValueChange(option.value)}
            className={cn(
              "relative z-10 inline-flex items-center justify-center rounded-[var(--radius-md)] font-medium transition-colors",
              paddingClasses[size],
              isActive
                ? "text-foreground"
                : "text-muted-foreground hover:text-foreground/80"
            )}
          >
            {isActive && (
              <motion.div
                layoutId="segmented-indicator"
                className="absolute inset-0 rounded-[var(--radius-md)] bg-[hsl(var(--surface-2))] shadow-sm"
                transition={{ type: "spring", stiffness: 400, damping: 30 }}
              />
            )}
            <span className="relative z-10">{option.label}</span>
          </button>
        );
      })}
    </div>
  );
}
