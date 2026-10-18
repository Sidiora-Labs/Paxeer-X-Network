import React from "react";
import type { SharePnLConfig } from "@paxeer-network/ui-share";
import { useDataListScript } from "./dataList.script";
import { DataList } from "./dataList.ui";

export const DataListWidget: React.FC<{
  symbol: string;
  className?: string;
  sharePnLConfig?: SharePnLConfig;
}> = (props) => {
  const state = useDataListScript(props);
  return <DataList {...state} className={props.className} />;
};
