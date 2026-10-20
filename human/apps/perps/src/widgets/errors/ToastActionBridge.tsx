/* ══════════════════════════════════════════════════════════════════════
   ToastAction bridge

   The existing toast atom doesn't export `ToastAction` because nothing
   used it before. We need an action-button primitive that:
     · Plugs into Radix `@radix-ui/react-toast` correctly (`altText`).
     · Inherits the brand button styling.
     · Closes the toast when activated by default.

   Keeping this in `widgets/errors` (not `ui/atoms`) so the toast atom
   stays minimal — only error infrastructure depends on it.
   ══════════════════════════════════════════════════════════════════════ */

import * as React from "react";
import * as ToastPrimitives from "@radix-ui/react-toast";
import { cn } from "@/ui/utils/cn";

export type ToastActionElement = React.ReactElement;

interface ToastActionProps
  extends React.ComponentPropsWithoutRef<typeof ToastPrimitives.Action> {
  /** Required by Radix for screen readers when the user clicks the action. */
  altText: string;
}

export const ToastAction = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Action>,
  ToastActionProps
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Action
    ref={ref}
    className={cn(
      "inline-flex h-7 shrink-0 items-center justify-center rounded-md",
      "border border-white/[0.12] bg-white/[0.04] px-2.5 text-[11px] font-medium",
      "transition-colors hover:bg-white/[0.08]",
      "focus:outline-none focus:ring-1 focus:ring-[color:var(--brand)]/50",
      "disabled:pointer-events-none disabled:opacity-50",
      "group-[.destructive]:border-destructive/40 group-[.destructive]:hover:border-destructive/60",
      "group-[.destructive]:hover:bg-destructive/10 group-[.destructive]:focus:ring-destructive",
      className,
    )}
    {...props}
  />
));
ToastAction.displayName = "ToastAction";
