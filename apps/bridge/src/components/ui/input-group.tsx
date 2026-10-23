import * as React from "react";

import { cn } from "@/lib/utils";

function InputGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="input-group"
      // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
      role="group"
      className={cn(
        "group/input-group relative flex h-10 w-full min-w-0 items-center rounded-md border border-input transition-[border-color,box-shadow] duration-150 ease-(--ease-dx) outline-none",
        "hover:border-foreground/30 has-[[data-slot=input-group-control]:focus-visible]:border-foreground has-[[data-slot=input-group-control]:focus-visible]:shadow-[0_0_0_3px_var(--outline-variant)]",
        "has-[textarea]:h-auto has-[[aria-invalid=true]]:border-destructive",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupAddon({
  className,
  align = "inline-start",
  ...props
}: React.ComponentProps<"div"> & { align?: "inline-start" | "inline-end" | "block-start" | "block-end" }) {
  return (
    // oxlint-disable-next-line jsx-a11y/no-static-element-interactions -- pointer convenience only; input remains keyboard focusable
    <div
      data-slot="input-group-addon"
      data-align={align}
      onMouseDown={(e) => {
        if ((e.target as HTMLElement).closest("button")) return;
        e.preventDefault();
        e.currentTarget.parentElement?.querySelector<HTMLInputElement>("input, textarea")?.focus();
      }}
      className={cn(
        "flex h-auto cursor-text items-center justify-center gap-2 py-1.5 text-sm font-medium text-muted-foreground select-none [&>svg:not([class*='size-'])]:size-4",
        align === "inline-start" && "order-first pl-3.5",
        align === "inline-end" && "order-last pr-2",
        align === "block-start" && "order-first w-full justify-start px-3.5 pt-3",
        align === "block-end" && "order-last w-full justify-start px-3.5 pb-3",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupInput({ className, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      data-slot="input-group-control"
      className={cn(
        "h-full min-w-0 flex-1 bg-transparent px-3.5 text-sm outline-none placeholder:text-muted-foreground/70 group-has-[[data-align=inline-start]]/input-group:pl-2 group-has-[[data-align=inline-end]]/input-group:pr-2",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupTextarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="input-group-control"
      className={cn("field-sizing-content min-h-16 w-full flex-1 resize-none bg-transparent px-3.5 py-3 text-sm outline-none placeholder:text-muted-foreground/70", className)}
      {...props}
    />
  );
}

function InputGroupText({ className, ...props }: React.ComponentProps<"span">) {
  return <span className={cn("flex items-center gap-2 text-sm text-muted-foreground [&_svg:not([class*='size-'])]:size-4", className)} {...props} />;
}

export { InputGroup, InputGroupAddon, InputGroupInput, InputGroupTextarea, InputGroupText };
