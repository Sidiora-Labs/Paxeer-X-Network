import { createContext, PropsWithChildren, useContext } from "react";
import { useEstLiqPriceBySymbol, useSymbolsInfo } from "@paxeer-network/hooks";
import { API, MarginMode, OrderSide } from "@paxeer-network/types";

export interface TPSLDetailContextState {
  base_dp: number;
  quote_dp: number;
  position: API.Position;
  side: OrderSide;
  estLiqPrice: number | undefined;
}

export const TPSLDetailContext = createContext({} as TPSLDetailContextState);

interface TPSLDetailProviderProps {
  symbol: string;
  position: API.Position;
}

export const TPSLDetailProvider = (
  props: PropsWithChildren<TPSLDetailProviderProps>,
) => {
  const symbolInfo = useSymbolsInfo()[props.symbol];
  const estLiqPrice = useEstLiqPriceBySymbol(
    props.symbol,
    props.position.margin_mode ?? MarginMode.CROSS,
  );

  return (
    <TPSLDetailContext.Provider
      value={{
        base_dp: symbolInfo("base_dp"),
        quote_dp: symbolInfo("quote_dp"),
        side: props.position.position_qty > 0 ? OrderSide.BUY : OrderSide.SELL,
        position: props.position,
        estLiqPrice: estLiqPrice,
      }}
    >
      {props.children}
    </TPSLDetailContext.Provider>
  );
};

export const useTPSLDetailContext = () => {
  return useContext(TPSLDetailContext);
};
