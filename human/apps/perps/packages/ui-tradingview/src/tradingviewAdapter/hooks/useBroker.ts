import { useRef, useEffect, useCallback } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import useCancelOrder from "../hooks/useCancelOrder";
import { ChartMode, ColorConfigInterface } from "../type";
import useEditOrder from "./useEditOrder";
import useSenderOrder from "./useSendOrder";

const createBrokerMethod = <T extends (...args: any) => any>(method: T) => {
  return (params: T) => method(params);
};

const useBroker = ({
  closeConfirm,
  colorConfig,
  onToast,
  mode,
  symbol,
}: {
  closeConfirm: any;
  colorConfig: ColorConfigInterface;
  onToast?: any;
  symbol: string;
  mode?: ChartMode;
}) => {
  const cancelOrder = useCancelOrder();
  const editOrder = useEditOrder(onToast);
  const symbolData = useSymbolsInfo();
  const closePosition = useCallback(
    (position: any) => closeConfirm && closeConfirm(position),
    [closeConfirm],
  );
  const { sendMarketOrder, sendLimitOrder } = useSenderOrder(symbol);

  const getSymbolInfo = useCallback(
    (symbol: string) => {
      if (!symbolData) {
        return;
      }
      return {
        baseMin: symbolData[symbol]("base_min"),
        baseMax: symbolData[symbol]("base_max"),
        baseTick: symbolData[symbol]("base_tick"),
        quoteTick: symbolData[symbol]("quote_tick"),
      };
    },
    [symbolData],
  );
  const broker = useRef({
    cancelOrder,
    closePosition,
    editOrder,
    colorConfig,
    sendLimitOrder,
    getSymbolInfo,
    sendMarketOrder: createBrokerMethod(sendMarketOrder),
    mode,
  });

  useEffect(() => {
    broker.current.getSymbolInfo = getSymbolInfo;
  }, [symbolData]);

  useEffect(() => {
    broker.current.sendLimitOrder = sendLimitOrder;
    broker.current.sendMarketOrder = sendMarketOrder;
  }, [sendLimitOrder, sendMarketOrder]);

  useEffect(() => {
    broker.current.closePosition = closePosition;
  }, [closeConfirm]);

  useEffect(() => {
    broker.current.cancelOrder = cancelOrder;
  }, [cancelOrder]);

  useEffect(() => {
    // when theme change, update the color config
    broker.current.colorConfig = colorConfig;
  }, [colorConfig]);

  return broker.current;
};

export default useBroker;
