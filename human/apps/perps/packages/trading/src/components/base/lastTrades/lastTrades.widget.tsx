import React from "react";
import { useLastTradesScript } from "./lastTrades.script";
import { LastTrades } from "./lastTrades.ui";

export const LastTradesWidget: React.FC<{
  symbol: string;
  classNames?: {
    root?: string;
    list?: string;
    listHeader?: string;
    listItem?: { left?: string; mid?: string; right?: string };
  };
  style?: React.CSSProperties;
}> = (props) => {
  const state = useLastTradesScript(props.symbol);
  return (
    <LastTrades {...state} classNames={props.classNames} style={props.style} />
  );
};
