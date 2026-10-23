import * as React from "react";

import { cn } from "@/lib/utils";

/** Twelve-blade spinner. Inherits `currentColor`, sized with `size-*`. */
function Spinner({ className, ...props }: React.ComponentProps<"output">) {
  return (
    <output
      aria-label="Loading"
      data-slot="spinner"
      className={cn("relative inline-block size-4 shrink-0", className)}
      {...props}
    >
      {Array.from({ length: 12 }, (_, i) => (
        <span
          key={i}
          className="absolute top-0 left-[46%] h-[28%] w-[8%] origin-[50%_178%] animate-[dx-spin-fade_1s_linear_infinite] rounded-full bg-current"
          style={{ transform: `rotate(${i * 30}deg)`, animationDelay: `${-1 + i / 12}s` }}
        />
      ))}
      <style>{`@keyframes dx-spin-fade{0%{opacity:1}100%{opacity:.15}}`}</style>
    </output>
  );
}

export { Spinner };
