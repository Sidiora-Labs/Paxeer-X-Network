import React from "react";
import type { PositionsProps } from "@paxeer-network/ui-positions";
import { DataListTabType, useDataListScript } from "./dataList.script";
import { DataList } from "./dataList.ui";

export const DataListWidget: React.FC<
  { current?: DataListTabType } & PositionsProps
> = (props) => {
  const state = useDataListScript(props);
  return <DataList {...state} />;
};
