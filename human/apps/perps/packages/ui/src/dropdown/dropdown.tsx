import * as React from "react";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import { tv, VariantProps } from "tailwind-variants";
import { SizeType } from "../helpers/sizeType";

const DropdownMenuRoot = DropdownMenuPrimitive.Root;

const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;

const DropdownMenuGroup = DropdownMenuPrimitive.Group;

const DropdownMenuPortal = DropdownMenuPrimitive.Portal;

const DropdownMenuSub = DropdownMenuPrimitive.Sub;

const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;

const dropdownMenuVariants = tv({
  slots: {
    content: [
      "oui-dropdown-menu-content",
      "oui-z-50",
      "oui-min-w-[8rem]",
      "oui-overflow-hidden",
      "oui-rounded-xl",
      "oui-p-1",
      "oui-bg-base-8",
      "oui-text-base-contrast",
      "oui-shadow-md",
      "oui-border",
      "oui-border-line-6",
      "data-[state=open]:oui-animate-in",
      "data-[state=closed]:oui-animate-out",
      "data-[state=closed]:oui-fade-out-0",
      "data-[state=open]:oui-fade-in-0",
      "data-[state=closed]:oui-zoom-out-95",
      "data-[state=open]:oui-zoom-in-95",
      "data-[side=bottom]:oui-slide-in-from-top-2",
      "data-[side=left]:oui-slide-in-from-right-2",
      "data-[side=right]:oui-slide-in-from-left-2",
      "data-[side=top]:oui-slide-in-from-bottom-2",
    ],
    item: [
      "oui-dropdown-menu-item",
      "oui-relative",
      "oui-flex",
      "oui-cursor-default",
      "oui-select-none",
      "oui-items-center",
      // "oui-rounded-lg",
      "oui-px-2",
      "oui-py-1.5",
      "oui-text-sm",
      "oui-outline-none",
      "oui-transition-colors",
      "hover:oui-bg-base-5",
      "focus:oui-bg-accent",
      "focus:oui-text-accent-foreground",
      "data-[disabled]:oui-pointer-events-none",
      "data-[disabled]:oui-opacity-50",
    ],
    label: "oui-px-2 oui-py-1.5 oui-text-sm oui-font-semibold",
    separator: "-oui-mx-1 oui-my-1 oui-h-px oui-bg-muted",
    shortcut: "oui-ms-auto oui-text-xs oui-tracking-widest oui-opacity-60",
  },
  variants: {
    size: {
      xs: {
        content: "oui-rounded",
        item: [
          "oui-px-2",
          "oui-py-1.5",
          "oui-h-6",
          "oui-rounded",
          "oui-text-2xs",
        ],
      },
      sm: {
        content: "oui-rounded",
        item: [
          "oui-px-2",
          "oui-py-1.5",
          "oui-h-6",
          "oui-rounded",
          "oui-text-2xs",
        ],
      },
      md: {
        content: "oui-rounded-md",
        item: ["oui-px-4", "oui-py-3", "oui-h-6", "oui-rounded"],
      },
      lg: {
        content: "oui-rounded-lg",
        item: ["oui-px-4", "oui-py-3", "oui-h-6", "oui-rounded-md"],
      },
      xl: {
        content: "oui-rounded-xl",
        item: [
          "oui-px-4",
          "oui-py-3",
          "oui-h-10",
          "oui-rounded-lg",
          "oui-text-base",
        ],
      },
    },
    inset: {
      true: {
        item: ["oui-ps-8"],
        label: ["oui-ps-8"],
      },
    },
  },
  defaultVariants: {
    inset: false,
    size: "lg",
  },
});

const DropdownMenuContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content> &
    VariantProps<typeof dropdownMenuVariants>
>(({ className, sideOffset = 4, size, ...props }, ref) => {
  const { content } = dropdownMenuVariants({ size });
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={content({ className })}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  );
});
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName;

const DropdownMenuItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & {
    inset?: boolean;
    size?: SizeType;
  }
>(({ className, inset, size, ...props }, ref) => {
  const { item } = dropdownMenuVariants({ size });
  return (
    <DropdownMenuPrimitive.Item
      ref={ref}
      className={item({ className, inset })}
      {...props}
    />
  );
});
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName;

// const DropdownMenuCheckboxItem = React.forwardRef<
//   React.ElementRef<typeof DropdownMenuPrimitive.CheckboxItem>,
//   React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.CheckboxItem>
// >(({ className, children, checked, ...props }, ref) => (
//   <DropdownMenuPrimitive.CheckboxItem
//     ref={ref}
//     className={cn(
//       "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
//       className
//     )}
//     checked={checked}
//     {...props}
//   >
//     <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
//       <DropdownMenuPrimitive.ItemIndicator>
//         {/* <CheckIcon className="h-4 w-4" /> */}
//       </DropdownMenuPrimitive.ItemIndicator>
//     </span>
//     {children}
//   </DropdownMenuPrimitive.CheckboxItem>
// ));
// DropdownMenuCheckboxItem.displayName =
//   DropdownMenuPrimitive.CheckboxItem.displayName;

// const DropdownMenuRadioItem = React.forwardRef<
//   React.ElementRef<typeof DropdownMenuPrimitive.RadioItem>,
//   React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.RadioItem>
// >(({ className, children, ...props }, ref) => (
//   <DropdownMenuPrimitive.RadioItem
//     ref={ref}
//     className={cn(
//       "relative flex cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
//       className
//     )}
//     {...props}
//   >
//     <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
//       <DropdownMenuPrimitive.ItemIndicator>
//         {/* <DotFilledIcon className="h-4 w-4 fill-current" /> */}
//       </DropdownMenuPrimitive.ItemIndicator>
//     </span>
//     {children}
//   </DropdownMenuPrimitive.RadioItem>
// ));
// DropdownMenuRadioItem.displayName = DropdownMenuPrimitive.RadioItem.displayName;

const DropdownMenuLabel = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label> & {
    inset?: boolean;
  }
>(({ className, inset, ...props }, ref) => {
  const { label } = dropdownMenuVariants({
    inset,
  });
  return (
    <DropdownMenuPrimitive.Label
      ref={ref}
      className={label({ className })}
      {...props}
    />
  );
});
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName;

const DropdownMenuSeparator = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(({ className, ...props }, ref) => {
  const { separator } = dropdownMenuVariants();
  return (
    <DropdownMenuPrimitive.Separator
      ref={ref}
      className={separator({ className })}
      {...props}
    />
  );
});
DropdownMenuSeparator.displayName = DropdownMenuPrimitive.Separator.displayName;

const DropdownMenuShortcut = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLSpanElement>) => {
  const { shortcut } = dropdownMenuVariants();
  return <span className={shortcut({ className })} {...props} />;
};
DropdownMenuShortcut.displayName = "DropdownMenuShortcut";

export {
  DropdownMenuRoot,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  //   DropdownMenuCheckboxItem,
  //   DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuGroup,
  DropdownMenuPortal,
  DropdownMenuSub,
  //   DropdownMenuSubContent,
  //   DropdownMenuSubTrigger,
  DropdownMenuRadioGroup,
};
