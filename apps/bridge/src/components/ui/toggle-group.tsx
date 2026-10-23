import * as React from "react";
import { Toggle as TogglePrimitive } from "@base-ui/react/toggle";
import { ToggleGroup as ToggleGroupPrimitive } from "@base-ui/react/toggle-group";
import { type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";
import { toggleVariants } from "@/components/ui/toggle";

const ToggleGroupContext = React.createContext<VariantProps<typeof toggleVariants>>({ size: "default", variant: "default" });

function ToggleGroup({
  className,
  variant,
  size,
  children,
  ...props
}: ToggleGroupPrimitive.Props & VariantProps<typeof toggleVariants>) {
  return (
    <ToggleGroupPrimitive
      data-slot="toggle-group"
      data-variant={variant}
      className={cn(
        "group/toggle-group inline-flex w-fit items-center gap-0.5 rounded-full",
        variant === "outline" ? "border border-border p-0.5" : "bg-container p-1",
        className,
      )}
      {...props}
    >
      <ToggleGroupContext.Provider value={{ variant, size }}>{children}</ToggleGroupContext.Provider>
    </ToggleGroupPrimitive>
  );
}

function ToggleGroupItem({ className, variant: _variant, size, ...props }: TogglePrimitive.Props & VariantProps<typeof toggleVariants>) {
  const ctx = React.useContext(ToggleGroupContext);
  return (
    <TogglePrimitive
      data-slot="toggle-group-item"
      className={cn(
        toggleVariants({ variant: "default", size: ctx.size ?? size }),
        "data-pressed:bg-background data-pressed:text-foreground data-pressed:shadow-[0_1px_2px_rgb(0_0_0/0.08),0_0_0_1px_var(--outline-variant)] dark:data-pressed:bg-container-highest",
        ctx.size === "sm" || size === "sm" ? "h-7 min-w-7" : "h-8 min-w-8",
        className,
      )}
      {...props}
    />
  );
}

export { ToggleGroup, ToggleGroupItem };
