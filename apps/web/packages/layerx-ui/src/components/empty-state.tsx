import * as React from "react";
import { cn } from "../lib/utils";

/**
 * Centered empty state — soft circular icon well, title, copy, optional CTA.
 * ("Ready to get started?" from the design set.)
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center gap-2.5 rounded-lg bg-surface px-6 py-10 text-center",
        className,
      )}
    >
      {icon && (
        <span className="mb-1 inline-flex size-16 items-center justify-center rounded-full bg-surface-sunken text-muted-foreground [&_svg]:size-7">
          {icon}
        </span>
      )}
      <h3 className="text-[17px] font-bold text-foreground">{title}</h3>
      {description && (
        <p className="max-w-[280px] text-sm leading-relaxed text-muted-foreground">{description}</p>
      )}
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}
