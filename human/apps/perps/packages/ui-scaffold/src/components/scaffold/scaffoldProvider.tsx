import React, { useContext, useMemo } from "react";
import {
  PaxeerNetworkContext,
  useChains,
  useMemoizedFn,
} from "@paxeer-network/hooks";
import { useAnnouncement } from "@paxeer-network/ui-notification";
import { checkChainSupport } from "../../utils/chain";
import { ScaffoldContext, ScaffoldState } from "./scaffoldContext";

export type ScaffoldProviderProps = Omit<
  ScaffoldState,
  "checkChainSupport" | "announcementState"
>;

export const ScaffoldProvider: React.FC<
  React.PropsWithChildren<ScaffoldProviderProps>
> = (props) => {
  const {
    routerAdapter,
    expanded,
    setExpand,
    topNavbarHeight,
    footerHeight,
    announcementHeight,
    children,
  } = props;

  const [chains] = useChains();

  const { networkId } = useContext<any>(PaxeerNetworkContext);

  const announcementState = useAnnouncement();

  // console.log("announcementState", announcementState);

  const checkChainSupportHandle = useMemoizedFn((chainId: number | string) => {
    return checkChainSupport(
      chainId,
      networkId === "testnet" ? chains.testnet : chains.mainnet,
    );
  });

  const memoizedValue = useMemo<ScaffoldState>(() => {
    return {
      routerAdapter: routerAdapter,
      expanded: expanded,
      setExpand: setExpand,
      checkChainSupport: checkChainSupportHandle,
      topNavbarHeight: topNavbarHeight,
      footerHeight: footerHeight,
      announcementHeight: announcementHeight,
      announcementState: announcementState,
    };
  }, [
    routerAdapter,
    expanded,
    setExpand,
    checkChainSupportHandle,
    topNavbarHeight,
    footerHeight,
    announcementHeight,
    announcementState,
  ]);

  return (
    <ScaffoldContext.Provider value={memoizedValue}>
      {children}
    </ScaffoldContext.Provider>
  );
};
