import { ScrollArea as ScrollAreaPrimitive } from "@base-ui/react/scroll-area";

import * as React from "react";

import { cn } from "@/lib/utils";

/** Mask that fades content under the edges only while there is more to scroll. */
const fadeMask = (size: number): React.CSSProperties => {
  const y0 = `min(${size}px, var(--scroll-area-overflow-y-start, 0px))`;
  const y1 = `min(${size}px, var(--scroll-area-overflow-y-end, 0px))`;
  const mask = `linear-gradient(to bottom, transparent 0, #000 ${y0}, #000 calc(100% - ${y1}), transparent 100%)`;
  return { maskImage: mask, WebkitMaskImage: mask };
};

type ScrollAreaProps = ScrollAreaPrimitive.Root.Props & {
  /** Fade content at the top/bottom edges while it overflows. Pass a number for the fade size in px. */
  fade?: boolean | number;
};

function ScrollArea({ className, children, fade, ...props }: ScrollAreaProps) {
  return (
    <ScrollAreaPrimitive.Root data-slot="scroll-area" className={cn("relative overflow-hidden", className)} {...props}>
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        className="size-full overscroll-contain rounded-[inherit] outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
        style={fade ? fadeMask(typeof fade === "number" ? fade : 40) : undefined}
      >
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollBar />
      <ScrollBar orientation="horizontal" />
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
}

function ScrollBar({ className, orientation = "vertical", ...props }: ScrollAreaPrimitive.Scrollbar.Props) {
  return (
    <ScrollAreaPrimitive.Scrollbar
      data-slot="scroll-area-scrollbar"
      orientation={orientation}
      className={cn(
        "pointer-events-none flex p-0.5 opacity-0 transition-opacity duration-200 select-none data-hovering:pointer-events-auto data-hovering:opacity-100 data-scrolling:pointer-events-auto data-scrolling:opacity-100 data-scrolling:duration-0",
        "data-[orientation=vertical]:w-2.5 data-[orientation=horizontal]:h-2.5 data-[orientation=horizontal]:flex-col",
        className,
      )}
      {...props}
    >
      <ScrollAreaPrimitive.Thumb data-slot="scroll-area-thumb" className="relative flex-1 rounded-full bg-[var(--scrollbar-thumb)] transition-colors hover:bg-[var(--scrollbar-thumb-hover)] active:bg-[var(--scrollbar-thumb-active)]" />
    </ScrollAreaPrimitive.Scrollbar>
  );
}

export { ScrollArea, ScrollBar };
