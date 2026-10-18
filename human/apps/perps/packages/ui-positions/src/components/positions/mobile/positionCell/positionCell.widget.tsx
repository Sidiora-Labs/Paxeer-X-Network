import { FC } from "react";
import { API } from "@paxeer-network/types";
import { PositionsProps } from "../../../../types/types";
import { usePositionCellScript } from "./positionCell.script";
import { PositionCell } from "./positionCell.ui";

export const PositionCellWidget: FC<
  {
    item: API.PositionTPSLExt;
    index: number;
    className?: string;
    shareIconSize?: number;
    positionReverse?: boolean;
  } & PositionsProps
> = (props) => {
  const state = usePositionCellScript(props);
  return <PositionCell {...state} className={props.className} />;
};
