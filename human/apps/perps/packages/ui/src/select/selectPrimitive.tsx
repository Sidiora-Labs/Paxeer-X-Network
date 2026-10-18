import * as React from "react";
import * as SelectPrimitive from "@radix-ui/react-select";
import { type VariantProps } from "tailwind-variants";
import { CaretDownIcon, CaretUpIcon } from "../icon";
import { tv } from "../utils/tv";

const selectVariants = tv(
  {
    slots: {
      trigger: [
        "oui-flex",
        "oui-group",
        "oui-w-full",
        "oui-items-center",
        "oui-justify-between",
        "oui-whitespace-nowrap",
        "oui-rounded-md",
        "oui-px-2",
        // "oui-py-2",

        "oui-gap-x-1",
        // "oui-text-sm",
        "oui-shadow-sm",
        // "oui-ring-offset-background",
        "oui-text-base-contrast-54",
        "placeholder:oui-text-base-contrast-54",
        "data-[state=open]:oui-text-base-contrast-80",
        "focus:oui-outline-none",
        "focus:oui-ring-1",
        "focus:oui-ring-ring",
        "disabled:oui-cursor-not-allowed",
        "disabled:oui-opacity-50",
        "[&>span]:oui-line-clamp-1",
      ],
      scrollUpButton:
        "oui-flex oui-cursor-default oui-items-center oui-justify-center oui-py-1",
      scrollDownButton:
        "oui-flex oui-cursor-default oui-items-center oui-justify-center oui-py-1",
      content: [
        "oui-relative",
        "oui-z-50",
        "oui-max-h-96",
        // "oui-min-w-[8rem]",
        "oui-overflow-hidden",
        "oui-rounded-md",
        "oui-bg-base-8",
        "oui-text-base-contrast",
        // "oui-bg-popover",
        // "oui-text-popover-foreground",
        "oui-shadow-md",
        "data-[state=open]:oui-animate-in",
        "data-[state=closed]:aoui-nimate-out",
        "data-[state=closed]:oui-fade-out-0",
        "data-[state=open]:oui-fade-in-0",
        "data-[state=closed]:oui-zoom-out-95",
        "data-[state=open]:oui-zoom-in-95",
        "data-[side=bottom]:oui-slide-in-from-top-2",
        "data-[side=left]:oui-slide-in-from-right-2",
        "data-[side=right]:oui-slide-in-from-left-2",
        "data-[side=top]:oui-slide-in-from-bottom-2",
      ],
      viewport: ["oui-p-1"],
      label: "oui-px-2 oui-py-1.5 oui-text-sm oui-font-semibold",
      item: [
        "oui-option-item",
        "oui-relative",
        "oui-flex",
        // "oui-w-full",
        "oui-cursor-default",
        "oui-select-none",
        "oui-items-center",
        "oui-rounded-sm",
        "oui-py-1",
        "oui-ps-2",
        "oui-pe-8",
        "oui-text-sm",
        "oui-text-base-contrast-54",
        "oui-outline-none",
        "hover:oui-bg-base-6",
        "hover:oui-rounded",
        "focus:oui-bg-accent",
        "focus:oui-text-accent-foreground",
        "data-[state=checked]:oui-bg-base-5",
        "data-[state=checked]:oui-text-base-contrast-80",
        "data-[disabled]:oui-pointer-events-none",
        "data-[disabled]:oui-opacity-50",
      ],
      separator: "-oui-mx-1 oui-my-1 oui-h-px oui-bg-muted",
      icon: "",
    },
    variants: {
      variant: {
        outlined: {
          trigger: ["oui-border oui-border-line-6 oui-bg-line-4"],
        },
        contained: {
          trigger: ["oui-bg-base-4"],
        },
        text: {
          trigger: [],
        },
        // text
      },
      position: {
        popper: {
          content: [
            "data-[side=bottom]:oui-translate-y-1",
            "data-[side=left]:-oui-translate-x-1",
            "data-[side=right]:oui-translate-x-1",
            "data-[side=top]:-oui-translate-y-1",
          ],
          viewport:
            "oui-h-[var(--radix-select-trigger-height)] oui-w-full oui-min-w-[var(--radix-select-trigger-width)]",
        },
        "item-aligned": {
          content: "",
          viewport: "",
        },
      },
      size: {
        xs: {
          trigger: ["oui-h-6", "oui-text-2xs"],
          item: ["oui-h-6", "oui-text-2xs"],
          icon: ["oui-w-3", "oui-h-3"],
        },
        sm: {
          trigger: ["oui-h-7", "oui-text-2xs"],
          item: ["oui-h-7", "oui-text-2xs"],
          icon: ["oui-w-4", "oui-h-4"],
        },
        md: {
          trigger: ["oui-h-8", "oui-text-xs"],
          item: ["oui-h-7", "oui-text-xs"],
          icon: ["oui-w-4", "oui-h-4"],
        },
        lg: {
          trigger: ["oui-h-10", "oui-text-sm", "oui-px-3"],
          item: ["oui-h-8", "oui-text-2xs"],
          icon: ["oui-w-5", "oui-h-5"],
        },
        xl: {
          trigger: ["oui-h-12", "oui-text-2xs", "oui-px-3"],
          item: ["oui-h-12", "oui-text-2xs"],
          icon: ["oui-w-6", "oui-h-6"],
        },
      },
      error: {
        true: {
          trigger: [
            "oui-border-danger",
            "focus:oui-ring-danger",
            "focus:oui-ring-ring-danger",
          ],
        },
      },
    },
    defaultVariants: {
      size: "lg",
      variant: "outlined",
    },
  },
  {
    responsiveVariants: ["md", "lg"],
  },
);

const SelectRoot = SelectPrimitive.Root;

const SelectGroup = SelectPrimitive.Group;

const SelectValue = SelectPrimitive.Value;

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger> &
    VariantProps<typeof selectVariants> & {
      showCaret?: boolean;
    }
>(
  (
    {
      className,
      children,
      size,
      error,
      variant,
      asChild,
      showCaret = true,
      ...props
    },
    ref,
  ) => {
    const { trigger } = selectVariants({ size, error, variant });
    if (asChild) {
      return (
        <SelectPrimitive.Trigger
          ref={ref}
          className={trigger({ className })}
          asChild={asChild}
          {...props}
        >
          {children}
        </SelectPrimitive.Trigger>
      );
    }
    return (
      <SelectPrimitive.Trigger
        ref={ref}
        className={trigger({ className })}
        asChild={asChild}
        {...props}
      >
        {children}

        <>
          {showCaret && (
            <SelectPrimitive.Icon
              asChild
              className="oui-transition-transform group-data-[state=open]:oui-rotate-180 group-data-[state=closed]:oui-rotate-0"
            >
              <CaretDownIcon
                size={12}
                className="oui-text-inherit"
                opacity={1}
              />
            </SelectPrimitive.Icon>
          )}
        </>
      </SelectPrimitive.Trigger>
    );
  },
);
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName;

const SelectScrollUpButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollUpButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollUpButton>
>(({ className, ...props }, ref) => {
  const { scrollUpButton } = selectVariants();
  return (
    <SelectPrimitive.ScrollUpButton
      ref={ref}
      className={scrollUpButton({ className })}
      {...props}
    >
      <CaretUpIcon size={16} color={"white"} />
    </SelectPrimitive.ScrollUpButton>
  );
});
SelectScrollUpButton.displayName = SelectPrimitive.ScrollUpButton.displayName;

const SelectScrollDownButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollDownButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollDownButton>
>(({ className, ...props }, ref) => {
  const { scrollDownButton } = selectVariants();
  return (
    <SelectPrimitive.ScrollDownButton
      ref={ref}
      className={scrollDownButton({ className })}
      {...props}
    >
      <CaretDownIcon size={16} color={"white"} />
    </SelectPrimitive.ScrollDownButton>
  );
});
SelectScrollDownButton.displayName =
  SelectPrimitive.ScrollDownButton.displayName;

const SelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => {
  const { content, viewport } = selectVariants({ position, className });
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        ref={ref}
        className={content({ className })}
        position={position}
        {...props}
      >
        {/* <SelectScrollUpButton /> */}
        <SelectPrimitive.Viewport className={viewport()}>
          {children}
        </SelectPrimitive.Viewport>
        {/* <SelectScrollDownButton /> */}
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
});
SelectContent.displayName = SelectPrimitive.Content.displayName;

const SelectLabel = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Label>
>(({ className, ...props }, ref) => {
  const { label } = selectVariants();
  return (
    <SelectPrimitive.Label
      ref={ref}
      className={label({ className })}
      {...props}
    />
  );
});
SelectLabel.displayName = SelectPrimitive.Label.displayName;

const SelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item> & {
    size?: VariantProps<typeof selectVariants>["size"];
  }
>(({ className, children, size, ...props }, ref) => {
  const { item } = selectVariants({ size });
  return (
    <SelectPrimitive.Item ref={ref} className={item({ className })} {...props}>
      {/* <span className="oui-absolute oui-end-2 oui-flex vh-3.5 oui-w-3.5 oui-items-center oui-justify-center">
        <SelectPrimitive.ItemIndicator>
          <CheckIcon size={16} />
        </SelectPrimitive.ItemIndicator>
      </span> */}
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  );
});
SelectItem.displayName = SelectPrimitive.Item.displayName;

const SelectSeparator = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>
>(({ className, ...props }, ref) => {
  const { separator } = selectVariants();
  return (
    <SelectPrimitive.Separator
      ref={ref}
      className={separator({ className })}
      {...props}
    />
  );
});
SelectSeparator.displayName = SelectPrimitive.Separator.displayName;

export {
  //   Select: ,
  SelectRoot,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
  SelectScrollUpButton,
  SelectScrollDownButton,
  selectVariants,
};
