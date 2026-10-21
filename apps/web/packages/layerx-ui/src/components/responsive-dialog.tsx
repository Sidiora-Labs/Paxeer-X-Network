"use client";

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { VisuallyHidden } from "@radix-ui/react-visually-hidden";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { cn } from "../lib/utils";
import { Sheet, SheetHeader, SheetBody, SheetFooter, SheetDescription } from "./sheet";
import { Modal, ModalHeader, ModalBody, ModalFooter } from "./modal";
import { Button, type ButtonProps } from "./button";

/**
 * One overlay, two bodies: bottom sheet on mobile, centered 440px modal on
 * desktop. This is the LayerX confirmation/detail contract from the pattern
 * table — consequence copy included, Esc/overlay dismiss, focus-trapped.
 */
export function ResponsiveDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  platform,
  portalContainer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  /** Consequence copy — shown under the title. */
  description?: React.ReactNode;
  children?: React.ReactNode;
  footer?: React.ReactNode;
  platform?: PlatformSetting;
  portalContainer?: HTMLElement | null;
}) {
  const resolved = usePlatform(platform);

  if (resolved === "mobile") {
    return (
      <Sheet open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
        <SheetHeader title={title} />
        <SheetBody className="flex flex-col gap-4">
          {description && <SheetDescription>{description}</SheetDescription>}
          {children}
        </SheetBody>
        {footer && <SheetFooter>{footer}</SheetFooter>}
      </Sheet>
    );
  }

  return (
    <Modal open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
      <ModalHeader title={title} description={description} />
      {children && <ModalBody>{children}</ModalBody>}
      {footer && <ModalFooter>{footer}</ModalFooter>}
    </Modal>
  );
}

/* -------------------------------------------------------- ConfirmDialog */

export interface ConfirmAction {
  label: React.ReactNode;
  onClick?: () => void;
  variant?: ButtonProps["variant"];
  loading?: boolean;
}

/**
 * Ready-made confirm: icon/title, consequence copy, and paired actions
 * (secondary | primary). Renders as a bottom sheet on mobile, a centered
 * modal on desktop.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  icon,
  title,
  consequence,
  confirm,
  cancel,
  platform,
  portalContainer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  icon?: React.ReactNode;
  title: React.ReactNode;
  /** The "what happens if you do this" copy. Required — it's the point. */
  consequence: React.ReactNode;
  confirm: ConfirmAction;
  cancel?: ConfirmAction;
  platform?: PlatformSetting;
  portalContainer?: HTMLElement | null;
}) {
  const resolved = usePlatform(platform);
  const footer = (
    <>
      {cancel && (
        <Button
          variant={cancel.variant ?? "secondary"}
          size="lg"
          fullWidth={resolved === "mobile"}
          loading={cancel.loading}
          onClick={cancel.onClick ?? (() => onOpenChange(false))}
        >
          {cancel.label}
        </Button>
      )}
      <Button
        variant={confirm.variant ?? "primary"}
        size="lg"
        fullWidth={resolved === "mobile"}
        loading={confirm.loading}
        onClick={confirm.onClick ?? (() => onOpenChange(false))}
      >
        {confirm.label}
      </Button>
    </>
  );

  const body = (
    <div className={cn("flex flex-col items-center gap-3 text-center", resolved === "desktop" && "py-2")}>
      {icon && (
        <span className="inline-flex size-16 items-center justify-center rounded-full bg-surface-sunken text-foreground-secondary [&_svg]:size-7">
          {icon}
        </span>
      )}
      <p className="text-[15px] leading-relaxed text-foreground-secondary">{consequence}</p>
    </div>
  );

  if (resolved === "mobile") {
    return (
      <Sheet open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
        <VisuallyHidden>
          <Dialog.Title>{title}</Dialog.Title>
        </VisuallyHidden>
        <SheetHeader />
        <SheetBody className="flex flex-col gap-4 pt-2">
          <h2 className="text-center text-xl font-bold text-foreground" aria-hidden>
            {title}
          </h2>
          {body}
        </SheetBody>
        <SheetFooter>{footer}</SheetFooter>
      </Sheet>
    );
  }

  return (
    <Modal open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
      <VisuallyHidden>
        <Dialog.Title>{title}</Dialog.Title>
      </VisuallyHidden>
      <div className="flex flex-col gap-4">
        <h2 className="text-center text-xl font-bold text-foreground" aria-hidden>
          {title}
        </h2>
        {body}
        <div className="mt-2 grid auto-cols-fr grid-flow-col gap-3">{footer}</div>
      </div>
    </Modal>
  );
}
