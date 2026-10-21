"use client";

import * as React from "react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { Button, type ButtonProps } from "./button";

/**
 * The screen's one primary CTA, placed per platform contract:
 * - mobile: full-width pill pinned at thumb reach (sticky footer with a
 *   soft gradient scrim above the tab bar / home indicator)
 * - desktop: fixed-width button — render it in the pane footer
 *   (`position="footer"`) or pass it to AppShell `headerActions`
 *   (`position="header"`).
 */
export function PrimaryAction({
  children,
  platform,
  position = "footer",
  className,
  ...props
}: ButtonProps & {
  platform?: PlatformSetting;
  position?: "footer" | "header";
}) {
  const resolved = usePlatform(platform);

  if (resolved === "mobile") {
    return (
      <div
        className={cn(
          "sticky bottom-0 z-20 -mx-4 mt-auto bg-[linear-gradient(to_top,var(--background)_60%,transparent)] px-4 pt-6 pb-[max(1rem,env(safe-area-inset-bottom))]",
        )}
      >
        <Button size="lg" fullWidth className={className} {...props}>
          {children}
        </Button>
      </div>
    );
  }

  return (
    <Button
      size={position === "header" ? "md" : "lg"}
      className={cn(position === "footer" && "min-w-[180px]", className)}
      {...props}
    >
      {children}
    </Button>
  );
}
