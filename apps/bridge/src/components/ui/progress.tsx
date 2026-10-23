import { Progress as ProgressPrimitive } from "@base-ui/react/progress";

import { cn } from "@/lib/utils";

function Progress({ className, children, ...props }: ProgressPrimitive.Root.Props) {
  return (
    <ProgressPrimitive.Root data-slot="progress" className={cn("flex w-full flex-wrap items-center gap-x-3 gap-y-2", className)} {...props}>
      {children}
      <ProgressPrimitive.Track data-slot="progress-track" className="relative h-1.5 w-full basis-full overflow-hidden rounded-full bg-container-highest">
        <ProgressPrimitive.Indicator
          data-slot="progress-indicator"
          className="h-full rounded-full bg-primary transition-[width] duration-500 ease-(--ease-dx) data-indeterminate:w-1/3 data-indeterminate:animate-[dx-indeterminate_1.4s_var(--ease-dx)_infinite]"
        />
      </ProgressPrimitive.Track>
    </ProgressPrimitive.Root>
  );
}

function ProgressLabel({ className, ...props }: ProgressPrimitive.Label.Props) {
  return <ProgressPrimitive.Label data-slot="progress-label" className={cn("text-sm font-medium", className)} {...props} />;
}

function ProgressValue({ className, ...props }: ProgressPrimitive.Value.Props) {
  return (
    <ProgressPrimitive.Value
      data-slot="progress-value"
      className={cn("ml-auto text-sm text-muted-foreground tabular-nums", className)}
      {...props}
    />
  );
}

export { Progress, ProgressLabel, ProgressValue };
