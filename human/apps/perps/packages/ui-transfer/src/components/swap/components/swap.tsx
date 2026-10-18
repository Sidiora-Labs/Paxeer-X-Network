import { FC } from "react";
import { API } from "@paxeer-network/types";
import { SwapMode, SymbolInfo } from "../types";
import { CrossSwap } from "./crossSwap";
import { SingleSwap } from "./singleSwap";

export interface SwapProps {
  mode: SwapMode;
  src: SymbolInfo;
  dst: SymbolInfo;
  chain?: API.NetworkInfos;
  nativeToken?: API.TokenInfo;
  depositFee?: bigint;
  transactionData: any;
  slippage: number;
  brokerName?: string;
  onComplete?: (isSuccss: boolean) => void;
  onCancel?: () => void;
  onFail?: () => void;
}

export const Swap: FC<SwapProps> = (props) => {
  const { mode } = props;

  if (mode === SwapMode.Cross) {
    return <CrossSwap {...props} />;
  }

  return <SingleSwap {...props} />;
};
