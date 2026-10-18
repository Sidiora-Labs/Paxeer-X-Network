import React from "react";
import { useFeeState, useRwaSymbolsInfoStore } from "@paxeer-network/hooks";
import { RegularFeesWidget } from "./regularFee";

export const FeesWidget: React.FC<{ symbol: string }> = ({ symbol }) => {
  const { takerFee, makerFee, rwaTakerFee, rwaMakerFee } = useFeeState();
  const info = useRwaSymbolsInfoStore();
  const isRwa = info?.[symbol] !== undefined;

  return (
    <RegularFeesWidget
      taker={isRwa ? rwaTakerFee : takerFee}
      maker={isRwa ? rwaMakerFee : makerFee}
    />
  );
};
