import React from "react";
import { LastTradesWidget } from "../../base/lastTrades";

export const MWebLastTrades: React.FC<{ symbol: string }> = (props) => {
  return (
    <LastTradesWidget
      symbol={props.symbol}
      classNames={{
        root: "oui-px-3 ",
        list: "oui-min-h-[141px] oui-max-h-[202px] oui-w-full",
        listHeader: "oui-text-xs oui-text-base-contrast-36",
        listItem: {
          left: "oui-text-xs",
          mid: "oui-text-xs",
          right: "oui-text-xs",
        },
      }}
    />
  );
};
