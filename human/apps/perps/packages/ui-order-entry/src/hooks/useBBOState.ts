import { useEffect, useMemo, useRef } from "react";
import { useLocalStorage, useTrack } from "@paxeer-network/hooks";
import {
  BBOOrderType,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
  TrackerEventName,
} from "@paxeer-network/types";
import {
  BBOStatus,
  getOrderLevelByBBO,
  getOrderTypeByBBO,
  isBBOOrder,
} from "../utils";

export function useBBOState({
  tpslSwitch,
  order_type,
  order_type_ext,
  side,
  setOrderValues,
}: {
  tpslSwitch: boolean;
  order_type?: OrderType;
  order_type_ext?: OrderType;
  side?: OrderSide;
  setOrderValues: (values: Partial<PaxeerNetworkOrder>) => void;
}) {
  const [localBBOType, setLocalBBOType] = useLocalStorage<
    BBOOrderType | undefined
  >("paxeerNetwork_order_bbo_type", undefined);

  const lastBBOType = useRef<BBOOrderType>(localBBOType);

  const { track } = useTrack();

  const bboStatus = useMemo(() => {
    if (
      tpslSwitch ||
      [OrderType.POST_ONLY, OrderType.IOC, OrderType.FOK].includes(
        order_type_ext!,
      )
    ) {
      return BBOStatus.DISABLED;
    }

    return localBBOType && order_type === OrderType.LIMIT
      ? BBOStatus.ON
      : BBOStatus.OFF;
  }, [tpslSwitch, order_type_ext, order_type, localBBOType]);

  const toggleBBO = () => {
    track(TrackerEventName.clickBBOButton);
    if (localBBOType) {
      // unselect bbo
      setLocalBBOType(undefined);
      // update formattedOrder values immediately instead of via useEffect
      setOrderValues({
        order_type_ext: undefined,
        level: undefined,
      });
    } else {
      setLocalBBOType(lastBBOType.current || BBOOrderType.COUNTERPARTY1);
    }
  };

  const onBBOChange = (value: BBOOrderType) => {
    setLocalBBOType(value);
    lastBBOType.current = value;
  };

  useEffect(() => {
    if (bboStatus === BBOStatus.DISABLED) {
      setOrderValues({
        // if order_type_ext is not bbo(ask, bid), keep previous value
        order_type_ext: isBBOOrder({ order_type_ext })
          ? undefined
          : order_type_ext,
        level: undefined,
      });
    }
  }, [bboStatus, order_type_ext]);

  useEffect(() => {
    if (bboStatus === BBOStatus.ON) {
      const orderType = getOrderTypeByBBO(localBBOType, side!);
      const orderLevel = getOrderLevelByBBO(localBBOType)!;
      setOrderValues({
        order_type_ext: orderType,
        level: orderLevel,
      });
    }
  }, [localBBOType, bboStatus, side]);

  return {
    bboStatus,
    bboType: localBBOType,
    setBBOType: setLocalBBOType,
    onBBOChange,
    toggleBBO,
  };
}
