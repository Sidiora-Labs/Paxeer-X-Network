import { useMemo } from "react";
import { useReferralInfo, useSymbolsInfo } from "@paxeer-network/hooks";
import { ReferralType, SharePnLOptions, SharePnLParams } from "../types/types";

export const useSharePnLScript = (props: {
  pnl?: SharePnLOptions & SharePnLParams;
  hide?: () => void;
}) => {
  const { pnl, hide } = props;
  const entity = pnl?.entity;
  const brokerName = pnl?.brokerName;
  const symbolInfo = useSymbolsInfo();
  const { getFirstRefCode } = useReferralInfo();
  const referralInfo = useMemo((): ReferralType | undefined => {
    const code = getFirstRefCode()?.code;
    const info = {
      code: pnl?.refCode ?? code,
      slogan: pnl?.refSlogan,
      link: pnl?.refLink,
    };
    return info;
  }, [getFirstRefCode, pnl]);

  // print warning if entity is null
  if (!entity) {
    console.warn("Entity is null, the share pnl will not be displayed");
  }

  // convert base_dp and quote_dp useMemo
  const base_dp = useMemo(() => {
    if (!entity) return undefined;
    return symbolInfo[entity?.symbol]("base_dp");
  }, [entity, symbolInfo]);
  const quote_dp = useMemo(() => {
    if (!entity) return undefined;
    return symbolInfo[entity?.symbol]("quote_dp");
  }, [entity, symbolInfo]);

  return {
    entity,
    baseDp: base_dp,
    quoteDp: quote_dp,
    referralInfo,
    shareOptions: pnl as SharePnLOptions,
    brokerName,
    hide,
  };
};

export type SharePnLState = ReturnType<typeof useSharePnLScript>;
