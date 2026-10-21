"use client";

import * as React from "react";
import { ArrowLeft, ChevronDown } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { Sheet, SheetHeader, SheetBody } from "./sheet";
import { Drawer, DrawerHeader, DrawerBody } from "./drawer";
import { IconButton } from "./button";

/**
 * Detail / education surface, per platform:
 * - mobile:  bottom sheet (variant="sheet", default) or a pushed full screen
 *            (variant="pushed") with a back header
 * - desktop: right-side drawer (variant="drawer", default) or an inline
 *            expanding section (variant="inline")
 */
export function DetailDisclosure({
  open,
  onOpenChange,
  title,
  children,
  mobileVariant = "sheet",
  desktopVariant = "drawer",
  platform,
  portalContainer,
  summary,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  children: React.ReactNode;
  mobileVariant?: "sheet" | "pushed";
  desktopVariant?: "drawer" | "inline";
  platform?: PlatformSetting;
  portalContainer?: HTMLElement | null;
  /** For inline variant: the always-visible summary row content. */
  summary?: React.ReactNode;
}) {
  const resolved = usePlatform(platform);
  const disclosureId = React.useId();

  // Inline expanding section (desktop docs-style)
  if (resolved === "desktop" && desktopVariant === "inline") {
    return (
      <div className="overflow-hidden rounded-lg border border-border bg-surface">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={disclosureId}
          onClick={() => onOpenChange(!open)}
          className="flex w-full items-center justify-between gap-3 px-5 py-4 text-left font-semibold text-foreground transition-colors hover:bg-surface-sunken/40"
        >
          <span>{summary ?? title}</span>
          <ChevronDown
            className={cn("size-4 text-muted-foreground transition-transform", open && "rotate-180")}
          />
        </button>
        <div
          id={disclosureId}
          role="region"
          className={cn(
            "grid transition-[grid-template-rows] duration-300",
            open ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
          )}
        >
          <div className="overflow-hidden">
            <div className="border-t border-border px-5 py-4">{children}</div>
          </div>
        </div>
      </div>
    );
  }

  if (resolved === "mobile" && mobileVariant === "pushed") {
    // Pushed full screen with back header (rendered above the shell)
    if (!open) return null;
    return (
      <div className="fixed inset-0 z-50 flex flex-col bg-background pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)] animate-fade-in">
        <header className="flex items-center gap-3 border-b border-border bg-surface px-4 py-3">
          <IconButton variant="outline" size="sm" onClick={() => onOpenChange(false)} aria-label="Back">
            <ArrowLeft />
          </IconButton>
          <h2 className="text-[17px] font-bold text-foreground">{title}</h2>
        </header>
        <div className="lx-scroll flex-1 overflow-y-auto p-4">{children}</div>
      </div>
    );
  }

  if (resolved === "mobile") {
    return (
      <Sheet open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
        <SheetHeader title={title} />
        <SheetBody>{children}</SheetBody>
      </Sheet>
    );
  }

  return (
    <Drawer open={open} onOpenChange={onOpenChange} portalContainer={portalContainer}>
      <DrawerHeader title={title} onClose={() => onOpenChange(false)} />
      <DrawerBody>{children}</DrawerBody>
    </Drawer>
  );
}
