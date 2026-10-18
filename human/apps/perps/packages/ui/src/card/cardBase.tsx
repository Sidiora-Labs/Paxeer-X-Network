import React from "react";
import { cnBase, type VariantProps } from "tailwind-variants";
import type { ComponentPropsWithout } from "../helpers/component-props";
import { decorationVariants } from "../layout/decoration";
import { tv } from "../utils/tv";

const cardVariants = tv({
  base: [
    "oui-card-root",
    "oui-card",
    "oui-rounded-xl",
    "oui-shadow",
    "oui-text-base-contrast",
    "oui-p-6",
  ],

  variants: {
    ...decorationVariants.variants,
  },
  defaultVariants: {
    intensity: 900,
  },
});

export type BaseCardProps = ComponentPropsWithout<"div", "color" | "title"> &
  VariantProps<typeof cardVariants>;

const CardBase = React.forwardRef<HTMLDivElement, BaseCardProps>(
  ({ className, intensity, children, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={cardVariants({ intensity, className })}
        {...props}
      >
        {children}
      </div>
    );
  },
);

CardBase.displayName = "CardBase";

const CardHeader = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, children, ...props }, ref) => (
  <div
    ref={ref}
    className={cnBase(
      "oui-card-header oui-flex oui-flex-col oui-space-y-1.5",
      className,
    )}
    {...props}
  >
    {children}
  </div>
));
CardHeader.displayName = "CardHeader";

const CardTitle = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLHeadingElement>
>(({ className, children, ...props }, ref) => (
  <h3
    ref={ref}
    className={cnBase(
      "oui-card-header-title oui-text-lg oui-font-semibold oui-leading-none oui-tracking-tight",
      className,
    )}
    {...props}
  >
    {children}
  </h3>
));

CardTitle.displayName = "CardTitle";

const CardDescription = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, children, ...props }, ref) => (
  <p
    ref={ref}
    className={cnBase("oui-text-muted-foreground oui-text-sm", className)}
    {...props}
  >
    {children}
  </p>
));

const CardContent = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, children, ...props }, ref) => (
  <div
    ref={ref}
    className={cnBase("oui-card-content oui-pt-4", className)}
    {...props}
  >
    {children}
  </div>
));

CardContent.displayName = "CardContent";

const CardFooter = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, children, ...props }, ref) => (
  <div ref={ref} className={className} {...props}>
    {children}
  </div>
));

CardFooter.displayName = "CardFooter";

export {
  CardBase,
  CardHeader,
  CardFooter,
  CardTitle,
  CardDescription,
  CardContent,
  cardVariants,
};
