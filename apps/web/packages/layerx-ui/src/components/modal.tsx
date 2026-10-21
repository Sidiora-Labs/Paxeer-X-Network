"use client";

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "../lib/utils";

export interface ModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
  portalContainer?: HTMLElement | null;
  className?: string;
}

/**
 * Desktop confirmation modal: centered, max 440px, rounded, Esc + overlay
 * dismiss, focus-trapped (Radix Dialog).
 */
export function Modal({ open, onOpenChange, children, portalContainer, className }: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={portalContainer ?? undefined}>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 data-[state=open]:animate-fade-in data-[state=closed]:animate-fade-out" />
        <Dialog.Content
          className={cn(
            "fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100dvh-2rem-env(safe-area-inset-top)-env(safe-area-inset-bottom))] w-[calc(100vw-2rem)] max-w-[440px] -translate-x-1/2 -translate-y-1/2 flex-col overscroll-contain",
            "rounded-xl bg-surface p-6 shadow-overlay outline-none",
            "data-[state=open]:animate-modal-in data-[state=closed]:animate-modal-out",
            className,
          )}
        >
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function ModalHeader({
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
    <div className={cn("flex items-start justify-between gap-4", className)}>
      <div className="flex flex-col gap-1.5">
        <Dialog.Title className="text-lg font-bold text-foreground">{title}</Dialog.Title>
        {description && (
          <Dialog.Description asChild>
            <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
          </Dialog.Description>
        )}
      </div>
      {onClose && (
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="-mt-1 -mr-1 inline-flex size-11 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-surface-sunken"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}

export function ModalBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("lx-scroll mt-4 flex-1 overflow-y-auto", className)} {...props} />;
}

export function ModalFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("mt-6 flex items-center justify-end gap-3", className)} {...props} />;
}
