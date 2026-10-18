import React from "react";
import { useOrderBookAndEntryScript } from "./orderBookAndEntry.script";
import { OrderBookAndEntry } from "./orderBookAndEntry.ui";

export const OrderBookAndEntryWidget: React.FC<{ className?: string }> = (
  props,
) => {
  const state = useOrderBookAndEntryScript();
  return <OrderBookAndEntry className={props.className} {...state} />;
};
