import { useWsStatus, WsNetworkStatus } from "@paxeer-network/hooks";

export type FooterReturns = {
  wsStatus: WsNetworkStatus;
};

export const useFooterScript = (): FooterReturns => {
  const wsStatus = useWsStatus();

  return {
    wsStatus,
  };
};
