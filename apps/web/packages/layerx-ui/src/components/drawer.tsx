"use client";

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "../lib/utils";

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
  portalContainer?: HTMLElement | null;
  /** Width of the right-side panel. Default 420px. */
  width?: number | string;
}

/**
 * Right-side drawer — the desktop detail/education surface.
 * Esc + overlay dismiss, focus-trapped.
 */
export function Drawer({ open, onOpenChange, children, portalContainer, width = 420 }: DrawerProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={portalContainer ?? undefined}>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/30 data-[state=open]:animate-fade-in data-[state=closed]:animate-fade-out" />
        <Dialog.Content
          style={{ width: `min(${typeof width === "number" ? `${width}px` : width}, 100vw)` }}
          className={cn(
            "fixed top-0 right-0 z-50 flex h-dvh flex-col overscroll-contain bg-surface pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)] shadow-overlay outline-none",
            "data-[state=open]:animate-drawer-in data-[state=closed]:animate-drawer-out",
          )}
        >
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function DrawerHeader({
  title,
  description,
  onClose,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  onClose?: () => void;
  className?: string;
}) {
  return (
    <div className={cn("flex items-start justify-between gap-4 border-b border-border p-5", className)}>
      <div className="flex flex-col gap-1">
        <Dialog.Title className="text-lg font-bold text-foreground">{title}</Dialog.Title>
        {description && (
          <Dialog.Description asChild>
            <p className="text-sm text-muted-foreground">{description}</p>
          </Dialog.Description>
        )}
      </div>
      {onClose && (
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-surface-sunken"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}

export function DrawerBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("lx-scroll flex-1 overflow-y-auto p-5", className)} {...props} />;
}

export function DrawerFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn("flex items-center justify-end gap-3 border-t border-border p-4", className)} {...props} />
  );
}
