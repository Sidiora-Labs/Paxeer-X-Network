import * as ResizablePrimitive from "react-resizable-panels";
import { cn } from "@paxeer-network/ui";

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      className={cn(
        "oui-flex oui-size-full aria-[orientation=vertical]:oui-flex-col",
        className,
      )}
      {...props}
    />
  );
}

function ResizablePanel({
  className,
  ...props
}: ResizablePrimitive.PanelProps) {
  return (
    <ResizablePrimitive.Panel
      data-slot="resizable-panel"
      className={cn(className)}
      {...props}
    />
  );
}

function ResizableHandle({
  withHandle,
  className,
  ...props
}: ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean;
}) {
  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      className={cn(
        "oui-relative oui-flex oui-w-px oui-items-center oui-justify-center oui-bg-primary after:oui-absolute after:oui-inset-y-0 after:oui-left-1/2 after:oui-w-1 after:-oui-translate-x-1/2 focus:oui-ring-0 focus-visible:oui-outline-none focus-visible:oui-ring-0 aria-[orientation=horizontal]:oui-h-px aria-[orientation=horizontal]:oui-w-full aria-[orientation=horizontal]:after:oui-left-0 aria-[orientation=horizontal]:after:oui-h-1 aria-[orientation=horizontal]:after:oui-w-full aria-[orientation=horizontal]:after:-oui-translate-y-1/2 aria-[orientation=horizontal]:after:oui-translate-x-0 [&[aria-orientation=horizontal]>div]:oui-rotate-90",
        className,
      )}
      {...props}
    >
      {withHandle && (
        <div className="oui-z-10 oui-flex oui-h-6 oui-w-1 oui-shrink-0 oui-rounded-lg oui-bg-primary" />
      )}
    </ResizablePrimitive.Separator>
  );
}

export { ResizableHandle, ResizablePanel, ResizablePanelGroup };
