import * as React from "react";

import { cn } from "@/lib/utils";

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn(
        "relative overflow-hidden rounded-md bg-container-high",
        "after:absolute after:inset-0 after:-translate-x-full after:animate-[dx-shimmer_1.6s_var(--ease-dx)_infinite] after:bg-linear-to-r after:from-transparent after:via-background/60 after:to-transparent",
        className,
      )}
      {...props}
    >
      <style>{`@keyframes dx-shimmer{100%{transform:translateX(100%)}}`}</style>
    </div>
  );
}

export { Skeleton };
