import * as React from "react";
import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import { tv } from "tailwind-variants";
import { CheckIcon } from "../icon/check";

const radioVariants = tv({
  slots: {
    group: "oui-grid oui-gap-2",
    item: [
      "oui-radio-item",
      "aspect-square",
      "h-4",
      "w-4",
      "rounded-full",
      "border",
      "border-primary-darken",
      "text-primary-darken",
      "shadow",
      "focus:outline-none",
      "focus-visible:ring-1",
      "focus-visible:ring-ring",
      "disabled:cursor-not-allowed",
      "disabled:opacity-50",
    ],
  },
});

const RadioGroup = React.forwardRef<
  React.ElementRef<typeof RadioGroupPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Root>
>(({ className, ...props }, ref) => {
  const { group } = radioVariants({
    className,
  });
  return <RadioGroupPrimitive.Root className={group()} {...props} ref={ref} />;
});
RadioGroup.displayName = RadioGroupPrimitive.Root.displayName;

const RadioGroupItem = React.forwardRef<
  React.ElementRef<typeof RadioGroupPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Item>
>(({ className, ...props }, ref) => {
  const { item } = radioVariants({
    className,
  });
  return (
    <RadioGroupPrimitive.Item ref={ref} className={item()} {...props}>
      <RadioGroupPrimitive.Indicator className="flex items-center justify-center">
        <CheckIcon className="h-3.5 w-3.5 fill-primary-darken" />
      </RadioGroupPrimitive.Indicator>
    </RadioGroupPrimitive.Item>
  );
});
RadioGroupItem.displayName = RadioGroupPrimitive.Item.displayName;

export { RadioGroup, RadioGroupItem };
