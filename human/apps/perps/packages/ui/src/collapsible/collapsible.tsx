import { FC } from "react";
import * as CollapsiblePrimitive from "@radix-ui/react-collapsible";
import { cn } from "..";

const Collapsible = CollapsiblePrimitive.Root;

const CollapsibleTrigger = CollapsiblePrimitive.CollapsibleTrigger;

const CollapsibleContent: FC<CollapsiblePrimitive.CollapsibleContentProps> = (
  props,
) => {
  return (
    <CollapsiblePrimitive.CollapsibleContent
      {...props}
      className={cn(
        props.className,
        "oui-overflow-hidden data-[state=closed]:oui-animate-collapsible-up data-[state=open]:oui-animate-collapsible-down",
      )}
    ></CollapsiblePrimitive.CollapsibleContent>
  );
};

export { Collapsible, CollapsibleTrigger, CollapsibleContent };
