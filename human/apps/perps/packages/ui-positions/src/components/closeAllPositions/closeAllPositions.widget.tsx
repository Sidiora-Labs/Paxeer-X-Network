import React from "react";
import { useCloseAllPositionsScript } from "./closeAllPositions.script";
import {
  CloseAllPositions,
  type CloseAllPositionsProps,
} from "./closeAllPositions.ui";

export type CloseAllPositionsWidgetProps = Pick<
  CloseAllPositionsProps,
  "className" | "style"
> & {
  symbol?: string;
  onSuccess?: () => void;
};

export const CloseAllPositionsWidget: React.FC<CloseAllPositionsWidgetProps> = (
  props,
) => {
  const { className, style, symbol, onSuccess } = props;
  const state = useCloseAllPositionsScript({ symbol, onSuccess });
  return <CloseAllPositions {...state} className={className} style={style} />;
};
