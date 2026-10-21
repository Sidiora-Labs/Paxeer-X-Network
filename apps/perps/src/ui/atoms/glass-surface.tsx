import * as React from "react";
import { cn } from "@/ui/utils/cn";

export interface GlassSurfaceProps extends React.HTMLAttributes<HTMLDivElement> {
  as?: React.ElementType;
  intensity?: "subtle" | "medium" | "strong";
}

const GlassSurface = React.forwardRef<HTMLDivElement, GlassSurfaceProps>(
  ({ className, as = "div", intensity = "medium", children, ...props }, ref) => {
    const intensityClasses = {
      subtle: "backdrop-blur-sm backdrop-saturate-[120%]",
      medium: "backdrop-blur-[20px] backdrop-saturate-[180%]",
      strong: "backdrop-blur-[40px] backdrop-saturate-[200%]",
    };

    const Comp: any = as;

    return (
      <Comp
        ref={ref}
        className={cn(
          "bg-[var(--surface-glass)] border border-[var(--glass-border)]",
          intensityClasses[intensity],
          className
        )}
        {...props}
      >
        {children}
      </Comp>
    );
  }
);
GlassSurface.displayName = "GlassSurface";

export { GlassSurface };
