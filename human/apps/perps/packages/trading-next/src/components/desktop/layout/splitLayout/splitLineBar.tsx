import React, { HTMLAttributes, useMemo } from "react";
import { SplitProps } from "@uiw/react-split";
import { cn } from "@paxeer-network/ui";

export type SplitLineBarProps = Pick<SplitProps, "mode"> &
  HTMLAttributes<HTMLDivElement>;

export const SplitLineBar: React.FC<SplitLineBarProps> = (props) => {
  const { onMouseDown, mode = "horizontal", ...rest } = props;

  const disable = useMemo(
    () => props.className?.split(" ").includes("disable"),
    [props.className],
  );

  const filterCls = useMemo(
    () => props.className?.split(" ").filter((cls) => cls !== "disable"),
    [props.className],
  );

  return (
    <div
      {...rest}
      className={cn(
        filterCls,
        "!oui-transition-none",
        "!oui-shadow-none !oui-bg-transparent",
        "hover:!oui-bg-primary-light hover:!oui-shadow-[0px_0px_4px_0px] hover:!oui-shadow-primary-light/80",
        "active:!oui-bg-primary-light active:!oui-shadow-[0px_0px_4px_0px] active:!oui-shadow-primary-light/80",
        "focus:!oui-bg-primary-light focus:!oui-shadow-[0px_0px_4px_0px] focus:!oui-shadow-primary-light/80",
        mode === "horizontal"
          ? "!oui-w-[2px] !oui-min-w-[2px] !oui-mx-[3px]"
          : "!oui-h-[2px] !oui-min-h-[2px] !oui-my-[3px]",
        disable && "oui-pointer-events-none",
      )}
    >
      <div
        onMouseDown={onMouseDown}
        className={cn(
          "!oui-transition-none",
          mode === "horizontal" ? "after:!oui-w-[2px]" : "after:!oui-h-[2px]",
          "after:!oui-bg-transparent after:!oui-shadow-transparent",
        )}
      />
    </div>
  );
};
