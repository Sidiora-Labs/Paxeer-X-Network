import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

function Tabs({ className, ...props }: TabsPrimitive.Root.Props) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col gap-3 data-[orientation=vertical]:flex-row", className)}
      {...props}
    />
  );
}

const tabsListVariants = cva("relative z-0 inline-flex w-fit items-center data-[orientation=vertical]:flex-col", {
  variants: {
    variant: {
      default: "gap-1 rounded-full bg-container p-1",
      line: "gap-5 border-b border-outline-variant",
    },
  },
  defaultVariants: { variant: "default" },
});

function TabsList({
  className,
  variant = "default",
  children,
  ...props
}: TabsPrimitive.List.Props & VariantProps<typeof tabsListVariants>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      data-variant={variant}
      className={cn("group/tabs-list", tabsListVariants({ variant }), className)}
      {...props}
    >
      {children}
      <TabsPrimitive.Indicator
        data-slot="tabs-indicator"
        className={cn(
          "absolute left-0 -z-1 translate-x-(--active-tab-left) transition-[translate,width,height] duration-250 ease-(--ease-dx)",
          variant === "line"
            ? "bottom-0 h-0.5 w-(--active-tab-width) rounded-full bg-foreground"
            : "top-1/2 h-(--active-tab-height) w-(--active-tab-width) -translate-y-1/2 rounded-full bg-background shadow-[0_1px_2px_rgb(0_0_0/0.08),0_0_0_1px_var(--outline-variant)] dark:bg-container-highest",
        )}
      />
    </TabsPrimitive.List>
  );
}

function TabsTrigger({ className, ...props }: TabsPrimitive.Tab.Props) {
  return (
    <TabsPrimitive.Tab
      data-slot="tabs-trigger"
      className={cn(
        "focus-ring inline-flex h-8 items-center justify-center gap-1.5 rounded-full px-3.5 text-[13px] font-medium whitespace-nowrap text-muted-foreground transition-colors duration-150 outline-none select-none hover:text-foreground data-active:text-foreground data-disabled:pointer-events-none data-disabled:opacity-40 [&_svg]:size-4 [&_svg]:shrink-0",
        "group-data-[variant=line]/tabs-list:h-10 group-data-[variant=line]/tabs-list:rounded-none group-data-[variant=line]/tabs-list:px-0.5",
        className,
      )}
      {...props}
    />
  );
}

function TabsContent({ className, ...props }: TabsPrimitive.Panel.Props) {
  return <TabsPrimitive.Panel data-slot="tabs-content" className={cn("flex-1 outline-none", className)} {...props} />;
}

export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants };
