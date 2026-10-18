import { forwardRef, useEffect, useState } from "react";
import {
  TooltipContent,
  TooltipPortal,
  TooltipProvider,
  TooltipRoot,
  TooltipArrow,
  TooltipTrigger,
} from "../tooltip";
import { Input, InputProps } from "./input";
import type { TooltipContentProps } from "@radix-ui/react-tooltip";

export type InputWithTooltipProps = InputProps & {
  tooltip?: React.ReactNode;
  tooltipProps?: {
    content?: TooltipContentProps;
    arrow?: TooltipContentProps;
  };
  triggerClassName?: string;
};

export const InputWithTooltip = forwardRef<
  HTMLInputElement,
  InputWithTooltipProps
>((props, ref) => {
  const { tooltip, tooltipProps, triggerClassName, ...inputProps } = props;
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (typeof tooltip !== "undefined" && tooltip !== "" && tooltip !== null) {
      setOpen(true);
    } else {
      setOpen(false);
    }
  }, [tooltip]);

  return (
    <TooltipRoot open={open}>
      <TooltipTrigger asChild>
        <div className={triggerClassName}>
          <Input {...inputProps} ref={ref} />
        </div>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent {...tooltipProps?.content}>
          {props.tooltip}
          <TooltipArrow {...tooltipProps?.arrow}/>
        </TooltipContent>
      </TooltipPortal>
    </TooltipRoot>
  );
});

InputWithTooltip.displayName = "InputWithTooltip";
