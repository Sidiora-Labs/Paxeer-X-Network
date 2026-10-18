import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { VariantProps, tv } from "tailwind-variants";
import { CloseIcon } from "../icon/close";

const dialogVariants = tv({
  slots: {
    overlay: [
      "oui-dialog-overlay",
      "oui-fixed",
      "oui-inset-0",
      "oui-z-50",
      "oui-bg-black/50",
      "oui-backdrop-blur-sm",
      "data-[state=open]:oui-animate-in",
      "data-[state=closed]:oui-animate-out",
      "data-[state=closed]:oui-fade-out-0",
      "data-[state=open]:oui-fade-in-0",
    ],
    content: [
      "oui-dialog-content",
      "oui-fixed",
      // RTL-EXCLUDE: Do not replace with `start-[50%]`.
      // Keep physical left centering here: start maps to right in RTL, which
      // breaks centering with translate-x-[-50%].
      "oui-left-[50%]",
      "oui-top-[50%]",
      "oui-z-50",
      // "oui-grid",
      "oui-w-full",
      "oui-max-w-lg",
      "oui-translate-x-[-50%]",
      "oui-translate-y-[-50%]",
      // "oui-gap-4",
      // "oui-bg-base-8",
      "oui-text-base-contrast-54",
      "oui-px-4 lg:oui-px-5",

      "oui-shadow-lg",
      "oui-duration-200",
      "data-[state=open]:oui-animate-in",
      "data-[state=closed]:oui-animate-out",
      "data-[state=closed]:oui-fade-out-0",
      "data-[state=open]:oui-fade-in-0",
      "data-[state=closed]:oui-zoom-out-95",
      "data-[state=open]:oui-zoom-in-95",
      "data-[state=closed]:oui-slide-out-to-left-1/2",
      "data-[state=closed]:oui-slide-out-to-top-[48%]",
      "data-[state=open]:oui-slide-in-from-left-1/2",
      "data-[state=open]:oui-slide-in-from-top-[48%]",
      "sm:oui-rounded-xl",
    ],
    body: ["oui-dialog-body", "oui-py-4 lg:oui-py-5", "oui-text-xs"],
    close: [
      "oui-dialog-close-btn",
      "oui-absolute",
      "oui-end-4",
      "oui-top-4",
      // "oui-opacity-70",
      "oui-ring-offset-background",
      "oui-transition-opacity",
      "hover:oui-opacity-100",

      "disabled:oui-pointer-events-none",
      // "data-[state=open]:oui-bg-accent",
      // "data-[state=open]:oui-text-muted-foreground",
    ],
    header: [
      "oui-dialog-header",
      "oui-flex",
      "oui-flex-col",
      // "oui-pb-4"
    ],
    footer: [
      "oui-dialog-footer",
      "oui-flex",
      "oui-flex-row",
      "oui-justify-end",
      // Use gap instead of space-x so spacing works in both LTR and RTL.
      "oui-gap-2",
      "oui-pb-5",
      "oui-pt-2 lg:oui-pt-3",
      // "has-[&>*:nth-child(1)]:oui-bg-red-400",
      // "oui-has-[:checked]:oui-bg-indigo-50",
      // "has-[button]:oui-bg-red-50",
    ],
    title: [
      "oui-dialog-title",
      "oui-text-base",
      // "oui-pt-3",
      // "oui-pb-2",
      "oui-min-h-11 lg:oui-min-h-[46px]",
      "oui-flex",
      "oui-items-center",
      "oui-text-base-contrast",
    ],
    desc: [
      "oui-dialog-desc",
      "oui-text-xs",
      "oui-text-warning-darken",
      "oui-pt-2",
      "oui-text-center",
      "oui-pb-3",
      // "has-[&_+_div]:oui-pb-3",
    ],
  },
  variants: {
    size: {
      xs: {
        content: ["oui-max-w-xs"], //320
      },
      sm: {
        content: ["oui-max-w-sm"], //360
      },
      md: {
        content: ["oui-max-w-md"], //420
      },
      lg: {
        content: ["oui-max-w-lg"], //480
      },
      xl: {
        content: ["oui-max-w-xl"], //540
      },
    },
    intensity: {
      100: {
        content: ["oui-bg-base-1"],
      },
      200: {
        content: ["oui-bg-base-2"],
      },
      300: {
        content: ["oui-bg-base-3"],
      },
      400: {
        content: ["oui-bg-base-4"],
      },
      500: {
        content: ["oui-bg-base-5"],
      },
      600: {
        content: ["oui-bg-base-6"],
      },
      700: {
        content: ["oui-bg-base-7"],
      },
      800: {
        content: ["oui-bg-base-8"],
      },
      900: {
        content: ["oui-bg-base-9"],
      },
    },
  },
  defaultVariants: {
    intensity: 800,
  },
});

const Dialog = DialogPrimitive.Root;

const DialogTrigger = DialogPrimitive.Trigger;

const DialogPortal = DialogPrimitive.Portal;

const DialogClose = DialogPrimitive.Close;

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => {
  const { overlay } = dialogVariants();
  return (
    <DialogPrimitive.Overlay
      ref={ref}
      className={overlay({ className })}
      {...props}
    />
  );
});
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

export type DialogContentProps = React.ComponentPropsWithoutRef<
  typeof DialogPrimitive.Content
> &
  VariantProps<typeof dialogVariants> & {
    closable?: boolean;
    onClose?: () => void;
    overlyClassName?: string;
  };

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  DialogContentProps
>(
  (
    {
      overlyClassName,
      className,
      children,
      size,
      closable = true,
      intensity,
      ...props
    },
    ref,
  ) => {
    const { content, close } = dialogVariants({ className, size, intensity });
    return (
      <DialogPortal>
        <DialogOverlay className={overlyClassName} />
        <DialogPrimitive.Content
          ref={ref}
          className={content({ className })}
          {...props}
        >
          {children}
          {closable && (
            <DialogPrimitive.Close
              className={close()}
              data-testid="oui-testid-dialog-content-close-btn"
            >
              <CloseIcon size={16} color="white" opacity={0.98} />
              <span className="oui-sr-only">Close</span>
            </DialogPrimitive.Close>
          )}
        </DialogPrimitive.Content>
      </DialogPortal>
    );
  },
);
DialogContent.displayName = DialogPrimitive.Content.displayName;

const DialogHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => {
  const { header } = dialogVariants();
  return <div className={header({ className })} {...props} />;
};
DialogHeader.displayName = "DialogHeader";

const DialogBody = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => {
  const { body } = dialogVariants();
  return <div className={body({ className })} {...props} />;
};

const DialogFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => {
  const { footer } = dialogVariants();
  return <div className={footer({ className })} {...props} />;
};
DialogFooter.displayName = "DialogFooter";

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => {
  const { title } = dialogVariants();
  return (
    <DialogPrimitive.Title
      ref={ref}
      className={title({ className })}
      {...props}
    />
  );
});
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => {
  const { desc } = dialogVariants();
  return (
    <DialogPrimitive.Description
      ref={ref}
      className={desc({ className })}
      {...props}
    />
  );
});
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogBody,
  DialogFooter,
  DialogTitle,
  DialogDescription,
};
