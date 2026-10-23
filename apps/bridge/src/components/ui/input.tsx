import { Input as InputPrimitive } from "@base-ui/react/input";

import { cn } from "@/lib/utils";

function Input({ className, type, ...props }: InputPrimitive.Props) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        "flex h-10 w-full min-w-0 rounded-md border border-input bg-transparent px-3.5 py-2 text-sm transition-[border-color,box-shadow,background-color] duration-150 ease-(--ease-dx) outline-none",
        "placeholder:text-muted-foreground/70 selection:bg-primary selection:text-primary-foreground",
        "hover:border-foreground/30 focus-visible:border-foreground focus-visible:shadow-[0_0_0_3px_var(--outline-variant)]",
        "file:text-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium",
        "disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-container disabled:opacity-60",
        "aria-invalid:border-destructive aria-invalid:shadow-[0_0_0_3px_color-mix(in_srgb,var(--destructive)_14%,transparent)] data-invalid:border-destructive",
        className,
      )}
      {...props}
    />
  );
}

export { Input };
