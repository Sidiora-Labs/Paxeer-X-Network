import React from "react";
import * as ScrollAreaPrimitive from "@radix-ui/react-scroll-area";
import type { VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";

const scrollAreaVariants = tv({
  slots: {
    root: "oui-relative oui-overflow-hidden oui-scroll-area-root",
    viewport: "oui-h-full oui-w-full oui-rounded-[inherit]",
    bar: [
      "oui-flex",
      "oui-touch-none",
      "oui-select-none",
      "oui-transition-colors",
    ],
    tumb: "oui-relative oui-flex-1 oui-rounded-full oui-bg-base-10",
  },
  variants: {
    orientation: {
      vertical: {
        bar: "oui-h-full oui-w-2 oui-border-s oui-border-s-transparent oui-p-[1px]",
      },
      horizontal: {
        bar: "oui-h-2 oui-flex-col oui-border-t oui-border-t-transparent oui-p-[1px]",
      },
    },
  },
});

interface ScrollAreaProps
  extends
    React.ComponentPropsWithoutRef<typeof ScrollAreaPrimitive.Root>,
    VariantProps<typeof scrollAreaVariants> {
  classNames?: {
    viewport?: string;
  };
}

const ScrollArea = React.forwardRef<
  React.ElementRef<typeof ScrollAreaPrimitive.Root>,
  ScrollAreaProps
>((oriProps, ref) => {
  const {
    className,
    classNames,
    children,
    orientation = "vertical",
    ...props
  } = oriProps;
  const { root, viewport } = scrollAreaVariants({ orientation });
  return (
    <ScrollAreaPrimitive.Root
      ref={ref}
      className={root({ className: className })}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        className={viewport({ className: classNames?.viewport })}
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar orientation={orientation} />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
});

ScrollArea.displayName = ScrollAreaPrimitive.Root.displayName;

const ScrollBar = React.forwardRef<
  React.ElementRef<typeof ScrollAreaPrimitive.ScrollAreaScrollbar>,
  React.ComponentPropsWithoutRef<typeof ScrollAreaPrimitive.ScrollAreaScrollbar>
>((oriProps, ref) => {
  const { className, orientation = "vertical", ...props } = oriProps;
  const { bar, tumb } = scrollAreaVariants({ className, orientation });
  return (
    <ScrollAreaPrimitive.ScrollAreaScrollbar
      ref={ref}
      orientation={orientation}
      className={bar({ className })}
      {...props}
    >
      <ScrollAreaPrimitive.ScrollAreaThumb className={tumb()} />
    </ScrollAreaPrimitive.ScrollAreaScrollbar>
  );
});

ScrollBar.displayName = ScrollAreaPrimitive.ScrollAreaScrollbar.displayName;

export { ScrollArea, ScrollBar, scrollAreaVariants };

export { type ScrollAreaProps };
