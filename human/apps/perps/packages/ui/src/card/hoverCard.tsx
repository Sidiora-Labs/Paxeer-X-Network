"use client";

import * as React from "react";
import * as HoverCardPrimitive from "@radix-ui/react-hover-card";
import { cn } from "..";

const HoverCardRoot = HoverCardPrimitive.Root;

const HoverCardTrigger = HoverCardPrimitive.Trigger;

const HoverCardContent = React.forwardRef<
  React.ElementRef<typeof HoverCardPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof HoverCardPrimitive.Content>
>(({ className, align = "center", sideOffset = 4, ...props }, ref) => (
  <HoverCardPrimitive.Content
    ref={ref}
    align={align}
    sideOffset={sideOffset}
    className={cn(
      "oui-z-[100] oui-w-64 oui-rounded-md oui-bg-base-8 oui-p-3 oui-text-base-contrast oui-shadow-md oui-outline-none data-[state=open]:oui-animate-in data-[state=closed]:oui-animate-out data-[state=closed]:oui-fade-out-0 data-[state=open]:oui-fade-in-0 data-[state=closed]:oui-zoom-out-95 data-[state=open]:oui-zoom-in-95 data-[side=bottom]:oui-slide-in-from-top-2 data-[side=left]:oui-slide-in-from-right-2 data-[side=right]:oui-slide-in-from-left-2 data-[side=top]:oui-slide-in-from-bottom-2",
      className,
    )}
    {...props}
  />
));
HoverCardContent.displayName = HoverCardPrimitive.Content.displayName;

export interface HoverCardProps
  extends React.ComponentPropsWithoutRef<typeof HoverCardPrimitive.Root> {
  className?: string;
  content: React.ReactNode;
}

const HoverCard: React.FC<
  HoverCardProps &
    Omit<
      React.ComponentPropsWithoutRef<typeof HoverCardPrimitive.Content>,
      "content"
    >
> = (props) => {
  const { content, open, defaultOpen, onOpenChange, children, ...rest } = props;
  return (
    <HoverCardRoot
      open={open}
      defaultOpen={defaultOpen}
      onOpenChange={onOpenChange}
    >
      <HoverCardTrigger asChild>{children}</HoverCardTrigger>
      <HoverCardPrimitive.Portal>
        <HoverCardContent {...rest}>
          {content}
          <HoverCardPrimitive.Arrow
            width={11}
            height={5}
            className="oui-fill-base-4"
          />
        </HoverCardContent>
      </HoverCardPrimitive.Portal>
    </HoverCardRoot>
  );
};

HoverCard.displayName = "HoverCard";

export { HoverCard, HoverCardRoot, HoverCardTrigger, HoverCardContent };
