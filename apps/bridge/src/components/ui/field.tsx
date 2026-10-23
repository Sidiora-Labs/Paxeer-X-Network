import * as React from "react";
import { Field as FieldPrimitive } from "@base-ui/react/field";
import { Fieldset as FieldsetPrimitive } from "@base-ui/react/fieldset";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const fieldVariants = cva("group/field flex w-full gap-2 data-invalid:text-destructive", {
  variants: {
    orientation: {
      vertical: "flex-col",
      horizontal: "flex-row items-center [&>[data-slot=field-label]]:flex-auto",
    },
  },
  defaultVariants: { orientation: "vertical" },
});

function Field({ className, orientation, ...props }: FieldPrimitive.Root.Props & VariantProps<typeof fieldVariants>) {
  return (
    <FieldPrimitive.Root
      data-slot="field"
      data-orientation={orientation ?? "vertical"}
      className={cn(fieldVariants({ orientation }), className)}
      {...props}
    />
  );
}

function FieldLabel({ className, ...props }: FieldPrimitive.Label.Props) {
  return (
    <FieldPrimitive.Label
      data-slot="field-label"
      className={cn("flex items-center gap-2 text-[13px] leading-snug font-medium text-foreground select-none group-data-disabled/field:opacity-50", className)}
      {...props}
    />
  );
}

function FieldControl({ className, ...props }: FieldPrimitive.Control.Props) {
  return (
    <FieldPrimitive.Control
      data-slot="field-control"
      className={cn(
        "flex h-10 w-full min-w-0 rounded-md border border-input bg-transparent px-3.5 py-2 text-sm text-foreground transition-[border-color,box-shadow] duration-150 outline-none placeholder:text-muted-foreground/70 hover:border-foreground/30 focus-visible:border-foreground focus-visible:shadow-[0_0_0_3px_var(--outline-variant)] data-invalid:border-destructive data-disabled:opacity-60",
        className,
      )}
      {...props}
    />
  );
}

function FieldDescription({ className, ...props }: FieldPrimitive.Description.Props) {
  return <FieldPrimitive.Description data-slot="field-description" className={cn("text-[13px] leading-normal text-muted-foreground", className)} {...props} />;
}

function FieldError({ className, ...props }: FieldPrimitive.Error.Props) {
  return <FieldPrimitive.Error data-slot="field-error" className={cn("text-[13px] font-medium text-destructive", className)} {...props} />;
}

function FieldSet({ className, ...props }: FieldsetPrimitive.Root.Props) {
  return <FieldsetPrimitive.Root data-slot="field-set" className={cn("flex flex-col gap-5", className)} {...props} />;
}

function FieldLegend({ className, ...props }: FieldsetPrimitive.Legend.Props) {
  return <FieldsetPrimitive.Legend data-slot="field-legend" className={cn("mb-1 text-[15px] font-medium", className)} {...props} />;
}

function FieldGroup({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="field-group" className={cn("flex w-full flex-col gap-6", className)} {...props} />;
}

function FieldSeparator({ className, children, ...props }: React.ComponentProps<"div">) {
  return (
    <div data-slot="field-separator" className={cn("relative flex items-center gap-3 text-xs text-muted-foreground", className)} {...props}>
      <span className="h-px flex-1 bg-outline-variant" />
      {children}
      {children && <span className="h-px flex-1 bg-outline-variant" />}
    </div>
  );
}

export { Field, FieldLabel, FieldControl, FieldDescription, FieldError, FieldSet, FieldLegend, FieldGroup, FieldSeparator };
