import * as React from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { type VariantProps } from "tailwind-variants";
import { tv } from "../utils/tv";

const popoverVariants = tv({
  base: [
    "oui-popover-content",
    "oui-z-50",
    "oui-w-72",
    "oui-rounded-md",
    "oui-border",
    "oui-border-line-6",
    // "oui-bg-popover",
    "oui-p-4",
    "oui-bg-base-8",
    // "oui-text-popover-foreground",
    "oui-shadow-md",
    "oui-outline-none",
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
});

const PopoverRoot = PopoverPrimitive.Root;

const PopoverTrigger = PopoverPrimitive.Trigger;

const PopoverAnchor = PopoverPrimitive.Anchor;

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof PopoverPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content> &
    VariantProps<typeof popoverVariants>
>(({ className, align = "center", sideOffset = 4, ...props }, ref) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      ref={ref}
      align={align}
      sideOffset={sideOffset}
      className={popoverVariants({ className })}
      {...props}
    />
  </PopoverPrimitive.Portal>
));
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

type PopoverProps = PopoverPrimitive.PopoverProps & {
  content: React.ReactNode;
  arrow?: boolean;
  contentProps?: PopoverPrimitive.PopoverContentProps;
};

/**
 * Popover component
 * @param {React.ReactNode} content - The content of the popover
 */
const Popover: React.FC<React.PropsWithChildren<PopoverProps>> = (props) => {
  const { arrow, content, contentProps, ...popoverProps } = props;
  return (
    <PopoverRoot {...popoverProps}>
      <PopoverTrigger asChild>{props.children}</PopoverTrigger>
      <PopoverContent {...contentProps}>
        {content}
        {arrow && (
          <PopoverPrimitive.Arrow
            className={"oui-fill-base-8"}
            width={10}
            height={6}
          />
        )}
      </PopoverContent>
    </PopoverRoot>
  );
};

export { PopoverRoot, PopoverTrigger, PopoverContent, PopoverAnchor, Popover };
