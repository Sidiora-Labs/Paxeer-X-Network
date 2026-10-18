import * as React from "react";
import * as SheetPrimitive from "@radix-ui/react-dialog";
import { cn, cnBase, tv, type VariantProps } from "tailwind-variants";
import { useDocumentDirection } from "../hooks/useDocumentDirection";
import { CloseIcon } from "../icon/close";

const Sheet = SheetPrimitive.Root;

const SheetTrigger = SheetPrimitive.Trigger;

const SheetClose = SheetPrimitive.Close;

const SheetPortal = SheetPrimitive.Portal;

const SheetOverlay = React.forwardRef<
  React.ElementRef<typeof SheetPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof SheetPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <SheetPrimitive.Overlay
    className={cnBase(
      "oui-sheet-overlay oui-fixed oui-inset-0 oui-z-50 oui-bg-black/50 oui-backdrop-blur-sm data-[state=open]:oui-animate-in data-[state=closed]:oui-animate-out data-[state=closed]:oui-fade-out-0 data-[state=open]:oui-fade-in-0",
      className,
    )}
    {...props}
    ref={ref}
  />
));
SheetOverlay.displayName = SheetPrimitive.Overlay.displayName;

const sheetVariants = tv({
  base: "oui-sheet-content oui-fixed oui-z-50 oui-gap-4 oui-bg-base-8 oui-px-4 oui-shadow-lg oui-transition oui-ease-in-out data-[state=closed]:oui-duration-260 data-[state=open]:oui-duration-300 data-[state=open]:oui-animate-in data-[state=closed]:oui-animate-out",
  variants: {
    side: {
      top: "oui-inset-x-0 oui-top-0 oui-border-b data-[state=closed]:oui-slide-out-to-top data-[state=open]:oui-slide-in-from-top",
      bottom:
        "oui-inset-x-0 oui-bottom-0 oui-rounded-t-2xl data-[state=closed]:oui-slide-out-to-bottom data-[state=open]:oui-slide-in-from-bottom",
      // Keep left/right sides on logical start/end. In RTL, callers expect
      // side="left" to attach to the visual start side; physical left/right
      // places drawers on the opposite side from their trigger.
      left: "oui-inset-y-0 oui-start-0 oui-h-full oui-w-3/4 sm:oui-max-w-sm",
      right:
        "oui-inset-y-0 oui-end-0 oui-h-full oui-w-3/4 oui-border-s sm:oui-max-w-sm",
    },
  },
  defaultVariants: {
    side: "bottom",
  },
});

function getLogicalSideAnimationClass(
  side: SheetContentProps["side"],
  direction: "ltr" | "rtl",
) {
  if (side === "left") {
    return direction === "rtl"
      ? "data-[state=closed]:oui-slide-out-to-right data-[state=open]:oui-slide-in-from-right"
      : "data-[state=closed]:oui-slide-out-to-left data-[state=open]:oui-slide-in-from-left";
  }

  if (side === "right") {
    return direction === "rtl"
      ? "data-[state=closed]:oui-slide-out-to-left data-[state=open]:oui-slide-in-from-left"
      : "data-[state=closed]:oui-slide-out-to-right data-[state=open]:oui-slide-in-from-right";
  }

  return undefined;
}

export interface SheetContentProps
  extends
    React.ComponentPropsWithoutRef<typeof SheetPrimitive.Content>,
    VariantProps<typeof sheetVariants> {
  // if true, show close button
  closeable?: boolean;
  onClose?: () => void;
  closeableSize?: number;
  closeOpacity?: number;
  closeClassName?: string;
  overlayClassName?: string;
}

const SheetContent = React.forwardRef<
  React.ElementRef<typeof SheetPrimitive.Content>,
  SheetContentProps
>(
  (
    {
      side = "bottom",
      closeable = true,
      closeableSize = 16,
      closeOpacity = 0.98,
      className,
      overlayClassName,
      children,
      ...props
    },
    ref,
  ) => {
    const direction = useDocumentDirection();
    const sideAnimationClassName = getLogicalSideAnimationClass(
      side,
      direction,
    );

    return (
      <SheetPortal>
        <SheetOverlay className={overlayClassName} />
        <SheetPrimitive.Content
          ref={ref}
          className={cnBase(
            sheetVariants({ side }),
            sideAnimationClassName,
            className,
          )}
          onOpenAutoFocus={(e) => e.preventDefault()}
          onCloseAutoFocus={(e) => e.preventDefault()}
          {...props}
        >
          {closeable && (
            // TODO(theme): "oui-ring-offset-base-700" is an invalid token (the base scale only defines 1-10) and generates no CSS. Remove it or replace with a valid token (e.g. "oui-ring-offset-base-7") after visual comparison.
            <SheetPrimitive.Close
              className={cnBase(
                "oui-sheet-close-btn oui-ring-offset-base-700 focus-visible:oui-ring-ring oui-absolute oui-end-4 oui-top-4 oui-z-10 oui-rounded-sm oui-transition-opacity focus-visible:oui-outline-none focus-visible:oui-ring-2 focus-visible:oui-ring-offset-2 active:oui-outline-none disabled:oui-pointer-events-none data-[state=open]:oui-bg-secondary",
                props?.closeClassName,
              )}
            >
              <CloseIcon
                size={closeableSize}
                color="white"
                opacity={closeOpacity}
                onClick={props?.onClose}
              />
              <span className="oui-sr-only">Close</span>
            </SheetPrimitive.Close>
          )}
          {children}
        </SheetPrimitive.Content>
      </SheetPortal>
    );
  },
);
SheetContent.displayName = SheetPrimitive.Content.displayName;

const SheetHeader = ({
  className,
  leading,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & {
  leading?: React.ReactNode;
}) => (
  <div
    className={cnBase(
      "oui-sheet-header oui-grid oui-min-h-12 oui-grid-cols-[40px_1fr_40px] oui-items-center",
    )}
  >
    <div>{leading}</div>
    <div
      {...props}
      className={cnBase(
        "oui-flex oui-flex-col oui-space-y-2 oui-text-center oui-text-lg oui-text-base-contrast",
        className,
      )}
    />
  </div>
);
SheetHeader.displayName = "SheetHeader";

const SheetBody = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => {
  return (
    <div className={cn("oui-py-4", className)({ twMerge: true })} {...props} />
  );
};

SheetBody.displayName = "DialogBody";

const SheetFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cnBase(
      "oui-sheet-footer oui-flex oui-flex-col-reverse sm:oui-flex-row sm:oui-justify-end sm:oui-gap-x-2",
      className,
    )}
    {...props}
  />
);
SheetFooter.displayName = "SheetFooter";

/**
 * Visible sheet title. Must render Radix `Dialog.Title` so `DialogContent` is
 * announced correctly to assistive tech (see Radix dialog a11y requirements).
 */
const SheetTitle = React.forwardRef<
  React.ElementRef<typeof SheetPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof SheetPrimitive.Title> & {
    leading?: React.ReactNode;
  }
>(({ className, leading, children, ...props }, ref) => (
  <div className="oui-sheet-header oui-grid oui-grid-cols-[40px_1fr_40px] oui-items-center">
    <div>{leading}</div>
    <SheetPrimitive.Title
      ref={ref}
      className={cnBase(
        "oui-flex oui-flex-col oui-space-y-2 oui-text-center oui-text-lg oui-text-base-contrast",
        className,
      )}
      {...props}
    >
      {children}
    </SheetPrimitive.Title>
  </div>
));
SheetTitle.displayName = SheetPrimitive.Title.displayName;

const SheetDescription = React.forwardRef<
  React.ElementRef<typeof SheetPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof SheetPrimitive.Description>
>(({ className, ...props }, ref) => (
  <SheetPrimitive.Description
    ref={ref}
    className={cnBase(
      "oui-sheet-description oui-text-2xs oui-text-base-contrast-54",
      className,
    )}
    {...props}
  />
));
SheetDescription.displayName = SheetPrimitive.Description.displayName;

export {
  Sheet,
  SheetPortal,
  SheetOverlay,
  SheetTrigger,
  SheetClose,
  SheetContent,
  SheetBody,
  SheetHeader,
  SheetFooter,
  SheetTitle,
  SheetDescription,
};
