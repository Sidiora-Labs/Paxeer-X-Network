import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import { VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";

const tabsVariants = tv({
  slots: {
    list: [
      "oui-tabs-list",
      "oui-header-list",
      "oui-flex",
      "oui-items-center",
      // "oui-px-1",
    ],
    content: [
      "oui-tabs-content",
      "oui-ring-offset-background",
      "focus-visible:oui-outline-none",
      "focus-visible:oui-ring-2",
      "focus-visible:oui-ring-ring",
      "focus-visible:oui-ring-offset-2",
    ],
    trigger: [
      "oui-tab-trigger",
      "oui-tabs-trigger",
      "oui-inline-flex",
      "oui-items-center",
      "oui-justify-center",
      "oui-whitespace-nowrap",
      //   "oui-px-3",

      "oui-box-content",
      "oui-font-medium",

      "oui-text-base-contrast-36 hover:oui-text-base-contrast-54",
      "oui-ring-offset-background",
      "oui-transition-all",
      /** `gap-x` mirrors correctly in RTL; `space-x` uses asymmetric margins that can overlap pill corners. */
      "oui-gap-x-1",
      "focus-visible:oui-outline-none",
      "focus-visible:oui-ring-2",
      "focus-visible:oui-ring-ring",
      "focus-visible:oui-ring-offset-2",
      "disabled:oui-pointer-events-none",
      "disabled:oui-opacity-50",
    ],
    icon: ["oui-text-inherit"],
  },
  variants: {
    variant: {
      text: {
        list: ["oui-gap-x-6", "oui-border-b", "oui-border-line-6", "oui-px-1"],
        trigger: [
          "oui-group",
          "oui-pb-2",
          "oui-relative",
          "data-[state=active]:oui-text-base-contrast",
          "data-[state=active]:after:oui-content-['']",
          "data-[state=active]:after:oui-block",
          "data-[state=active]:after:oui-h-[3px]",
          "data-[state=active]:after:oui-bg-base-contrast",
          "data-[state=active]:after:oui-absolute",
          "data-[state=active]:after:oui-rounded-full",
          "data-[state=active]:after:-oui-bottom-0",
          "data-[state=active]:after:oui-start-0",
          "data-[state=active]:after:oui-end-0",
        ],
      },
      contained: {
        list: ["oui-gap-x-[6px]"],
        trigger: [
          "oui-group",
          /** `!rounded` defeats host/segment overrides that strip one physical corner under `[dir="rtl"]` + `:first-child`. */
          "!oui-rounded",
          "oui-px-3",
          "oui-bg-base-7 hover:oui-bg-base-5",
          "oui-text-base-contrast-36",
          "data-[state=active]:oui-bg-base-5",
          "data-[state=active]:oui-text-base-contrast",
        ],
        icon: ["oui-text-inherit"],
      },
    },
    size: {
      sm: {},
      md: {},
      lg: {},
      xl: {},
    },
  },
  compoundVariants: [
    {
      size: "sm",
      variant: "text",
      className: {
        trigger: ["oui-text-sm", "oui-h-5"],
        icon: ["oui-w-[10px]", "oui-h-[10px]"],
      },
    },
    {
      size: "md",
      variant: "text",
      className: {
        trigger: ["oui-text-base", "oui-h-6"],
        icon: ["oui-w-3", "oui-h-3"],
      },
    },
    {
      size: "lg",
      variant: "text",
      className: {
        trigger: ["oui-text-lg", "oui-h-7"],
        icon: ["oui-w-[14px]", "oui-h-[14px]"],
      },
    },
    {
      size: "xl",
      variant: "text",
      className: {
        trigger: ["oui-text-lg", "oui-h-7"],
        icon: ["oui-w-4", "oui-h-4"],
      },
    },
    {
      size: "sm",
      variant: "contained",
      className: {
        list: ["oui-gap-x-1"],
        trigger: ["oui-text-2xs", "oui-h-6"],
        icon: ["oui-w-[10px]", "oui-h-[10px]"],
      },
    },
    {
      size: "md",
      variant: "contained",
      className: {
        // list: ["oui-space-x-[6px]"],
        trigger: ["oui-text-2xs", "oui-h-7"],
        icon: ["oui-w-3", "oui-h-3"],
      },
    },
    {
      size: "lg",
      variant: "contained",
      className: {
        trigger: ["oui-text-sm", "oui-h-8", "!oui-rounded-md"],
        icon: ["oui-w-[14px]", "oui-h-[14px]"],
      },
    },
    {
      size: "xl",
      variant: "contained",
      className: {
        trigger: ["oui-text-base", "oui-h-9", "!oui-rounded-md"],
        icon: ["oui-w-4", "oui-h-4"],
      },
    },
  ],
  defaultVariants: {
    size: "md",
    variant: "text",
  },
});

const TabsBase = TabsPrimitive.Root;

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List> &
    VariantProps<typeof tabsVariants>
>((originProps, ref) => {
  const { className, size, variant, ...props } = originProps;
  const { list } = tabsVariants({ size, variant });
  return (
    <TabsPrimitive.List ref={ref} className={list({ className })} {...props} />
  );
});

TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger> &
    VariantProps<typeof tabsVariants> & {
      icon?: React.ReactElement;
      "data-testid"?: string;
    }
>((originProps, ref) => {
  const { className, size, children, icon, variant, ...props } = originProps;
  const { trigger, icon: iconClassName } = tabsVariants({ size, variant });
  return (
    <TabsPrimitive.Trigger
      ref={ref}
      className={trigger({ className })}
      {...props}
    >
      {typeof icon !== "undefined"
        ? React.cloneElement(icon, { className: iconClassName(), opacity: 1 })
        : null}
      <span>{children}</span>
    </TabsPrimitive.Trigger>
  );
});

TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content> &
    VariantProps<typeof tabsVariants>
>((oriProps, ref) => {
  const { className, size, children, ...props } = oriProps;
  const { content } = tabsVariants({ size });
  return (
    <TabsPrimitive.Content
      ref={ref}
      className={content({ className })}
      {...props}
    >
      {children}
    </TabsPrimitive.Content>
  );
});

TabsContent.displayName = TabsPrimitive.Content.displayName;

export { TabsBase, TabsList, TabsTrigger, TabsContent, tabsVariants };
