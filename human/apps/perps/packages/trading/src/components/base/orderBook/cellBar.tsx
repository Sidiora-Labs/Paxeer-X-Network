import { FC, useMemo } from "react";
import { cn } from "@paxeer-network/ui";

export enum CellBarDirection {
  LEFT_TO_RIGHT,
  RIGHT_TO_LEFT,
}

interface CellBarProps {
  width: number;
  className?: string;
  direction?: CellBarDirection;
}

export const CellBar: FC<CellBarProps> = (props) => {
  const { direction = CellBarDirection.RIGHT_TO_LEFT } = props;
  const transform = useMemo(() => {
    const x = Math.max(props.width, 0);
    if (direction === CellBarDirection.LEFT_TO_RIGHT) {
      return { transform: `translateX(${x}%)` };
    }
    return { transform: `translateX(-${x}%)` };
  }, [props.width]);

  return (
    <div
      className={cn(
        "oui-absolute oui-end-[-100%] oui-top-0 oui-h-full oui-w-full oui-transition-transform oui-pointer-events-none",
        direction === CellBarDirection.LEFT_TO_RIGHT && "oui-start-[-100%]",
        props.className,
      )}
      style={transform}
    />
  );
};
