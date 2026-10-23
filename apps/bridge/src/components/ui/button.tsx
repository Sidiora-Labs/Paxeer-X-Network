import * as React from "react";
import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "state-layer focus-ring inline-flex shrink-0 items-center justify-center gap-2 rounded-full font-medium whitespace-nowrap select-none transition-[background-color,color,box-shadow,transform] duration-150 ease-(--ease-dx) active:not-disabled:scale-[0.98] disabled:pointer-events-none data-disabled:pointer-events-none disabled:opacity-40 data-disabled:opacity-40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-primary primary-fill text-primary-foreground [--state-hover:0.12] [--state-press:0.18] [--state-focus:0.2]",
        tonal: "bg-secondary text-secondary-foreground",
        outline: "border border-border bg-transparent text-foreground",
        ghost: "bg-transparent text-foreground",
        protected:
          "bg-background/70 text-foreground shadow-[0_0_0_1px_var(--outline-variant)] backdrop-blur-xl",
        destructive:
          "bg-destructive text-destructive-foreground [--state-hover:0.12] [--state-press:0.18]",
        link: "h-auto! rounded-xs px-0! text-muted-foreground underline-offset-[0.2em] before:hidden hover:text-foreground hover:underline",
      },
      size: {
        xs: "h-7 px-3 text-xs [&_svg:not([class*='size-'])]:size-3.5",
        sm: "h-8 px-3.5 text-[13px]",
        default: "h-10 px-5 text-sm",
        lg: "h-12 px-7 text-[15px]",
        icon: "size-10",
        "icon-xs": "size-7 [&_svg:not([class*='size-'])]:size-3.5",
        "icon-sm": "size-8",
        "icon-lg": "size-12 [&_svg:not([class*='size-'])]:size-5",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

type ButtonProps = ButtonPrimitive.Props & VariantProps<typeof buttonVariants>;

/** `render={<a />}` / `render={<Link />}` → not a native button, so Base UI skips button-only semantics. */
function isNativeButton(render: ButtonProps["render"]) {
  if (!render || typeof render === "function") return true;
  return React.isValidElement(render) && render.type === "button";
}

function Button({ className, variant, size, render, nativeButton, ...props }: ButtonProps) {
  return (
    <ButtonPrimitive
      data-slot="button"
      render={render}
      nativeButton={nativeButton ?? isNativeButton(render)}
      data-variant={variant ?? "default"}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants, type ButtonProps };
