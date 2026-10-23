import { Toaster as Sonner, toast, type ToasterProps } from "sonner";
import { CircleCheckIcon, InfoIcon, Loader2Icon, OctagonXIcon, TriangleAlertIcon } from "lucide-react";

import { useTheme } from "@/components/theme-provider";

function Toaster(props: ToasterProps) {
  const { resolvedTheme } = useTheme();
  return (
    <Sonner
      theme={resolvedTheme}
      className="toaster group"
      icons={{
        success: <CircleCheckIcon className="size-4 text-success" />,
        info: <InfoIcon className="size-4 text-info" />,
        warning: <TriangleAlertIcon className="size-4 text-warning" />,
        error: <OctagonXIcon className="size-4 text-destructive" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      toastOptions={{
        classNames: {
          toast:
            "group toast font-sans! rounded-lg! border! border-outline-variant! bg-popover! text-popover-foreground! shadow-float! gap-3! px-4! py-3.5!",
          title: "text-sm! font-medium!",
          description: "text-[13px]! text-muted-foreground!",
          actionButton: "rounded-full! bg-primary! text-primary-foreground! h-7! px-3! text-xs! font-medium!",
          cancelButton: "rounded-full! bg-secondary! text-secondary-foreground! h-7! px-3! text-xs!",
        },
      }}
      {...props}
    />
  );
}

export { Toaster, toast };
