"use client";

import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../lib/utils";

const avatarVariants = cva(
  "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full font-semibold select-none",
  {
    variants: {
      size: {
        xs: "size-7 text-[11px]",
        sm: "size-9 text-xs",
        md: "size-11 text-sm",
        lg: "size-14 text-base",
        xl: "size-20 text-xl",
      },
      tone: {
        /** Black tile with white initials — the design set's profile avatar. */
        primary: "bg-primary text-primary-foreground",
        accent: "bg-accent-soft text-accent-strong",
        neutral: "bg-surface-sunken text-foreground-secondary",
      },
    },
    defaultVariants: { size: "md", tone: "neutral" },
  },
);

export interface AvatarProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof avatarVariants> {
  src?: string;
  alt?: string;
  /** Fallback initials; derived from `alt` when omitted. */
  initials?: string;
}

function deriveInitials(name?: string) {
  if (!name) return "";
  return name
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]!.toUpperCase())
    .join("");
}

const Avatar = React.forwardRef<HTMLSpanElement, AvatarProps>(
  ({ className, size, tone, src, alt, initials, ...props }, ref) => {
    const [imgFailed, setImgFailed] = React.useState(false);
    const showImage = src && !imgFailed;
    return (
      <span ref={ref} className={cn(avatarVariants({ size, tone, className }))} {...props}>
        {showImage ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={src}
            alt={alt ?? ""}
            className="absolute inset-0 size-full object-cover"
            onError={() => setImgFailed(true)}
          />
        ) : (
          <span aria-hidden>{initials ?? deriveInitials(alt)}</span>
        )}
      </span>
    );
  },
);
Avatar.displayName = "Avatar";

export { Avatar, avatarVariants };
