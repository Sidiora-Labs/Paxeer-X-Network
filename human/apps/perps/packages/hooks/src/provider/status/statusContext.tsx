import { createContext, useContext } from "react";
import { type WsNetworkStatus } from "../../useWsStatus";

export interface StatusContextState {
  ws?: WsNetworkStatus;
}

export const StatusContext = createContext<StatusContextState>({});

export const useStatusContext = () => {
  return useContext(StatusContext);
};
