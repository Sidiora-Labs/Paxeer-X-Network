import { useCallback, useEffect, useRef, useState } from "react";
import { Environment, createClient } from "@layerzerolabs/scan-client";
import { pick } from "ramda";
import { utils } from "@paxeer-network/core";
import {
  useAccountInstance,
  useBoolean,
  useConfig,
  useEventEmitter,
} from "@paxeer-network/hooks";
import { WS_WalletStatusEnum } from "@paxeer-network/types";
import {
  isNativeTokenChecker,
  woofiDexCrossSwapChainRouterAbi,
} from "../constants";

export enum MessageStatus {
  INITIALIZING = "WAITTING",
  INFLIGHT = "INFLIGHT",
  CONFIRMING = "CONFIRMING",
  DELIVERED = "DELIVERED",
  FAILED = "FAILED",
}

export const useCrossSwap = (): any => {
  const [loading, { setTrue: start, setFalse: stop }] = useBoolean(false);

  const [layerStatus, setLayerStatus] = useState<MessageStatus>(
    MessageStatus.INITIALIZING,
  );

  const ee = useEventEmitter();

  const [bridgeMessage, setBridgeMessage] = useState<any | undefined>();

  const [status, setStatus] = useState<WS_WalletStatusEnum>(
    WS_WalletStatusEnum.NO,
  );
  const txHashFromBridge = useRef<string | undefined>();

  const checkLayerStatusListener = useRef<ReturnType<typeof setTimeout>>();

  const account = useAccountInstance();

  const config = useConfig();
  const brokerId = config.get("brokerId");
  const networkId = config.get("networkId");

  const client = useRef(createClient(networkId as Environment)).current;
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>();

  useEffect(() => {
    const handler = (data: any) => {
      const { side, transStatus, trxId } = data;

      if (side === "DEPOSIT" && trxId === txHashFromBridge.current) {
        setStatus(transStatus);
      }
    };
    ee.on("wallet:changed", handler);

    return () => {
      ee.off("wallet:changed", handler);
    };
  }, [txHashFromBridge.current]);

  const checkLayerStatus = useCallback((txHash: string) => {
    if (checkLayerStatusListener.current) {
      clearTimeout(checkLayerStatusListener.current);
    }

    checkLayerStatusListener.current = setTimeout(async () => {
      try {
        const { messages } = await client.getMessagesBySrcTxHash(txHash);

        if (messages.length > 0) {
          const { status } = messages[0];

          setLayerStatus(status as MessageStatus);

          if (status === MessageStatus.DELIVERED) {
            setBridgeMessage(messages[0]);
            txHashFromBridge.current = messages[0].dstTxHash;
          } else if (status === MessageStatus.FAILED) {
            setBridgeMessage(messages[0]);
          } else {
            checkLayerStatus(txHash);
          }
        } else {
          checkLayerStatus(txHash);
        }
      } catch (e) {
        // setLayerStatus(MessageStatus.FAILED);
        checkLayerStatus(txHash);
      }
    }, 1000);
  }, []);

  // swap => src tx hash, cross swap => dst tx hash
  // const checkDeposit

  useEffect(() => {
    return () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    };
  }, []);

  const dstValutDeposit = useCallback(() => {
    return {
      accountId: account.accountIdHashStr,
      brokerHash: utils.parseBrokerHash(brokerId),
      tokenHash: utils.parseTokenHash("USDC"),
    };
  }, [account, brokerId]);

  const swap = async (inputs: {
    address: string;
    crossChainRouteAddress: string;
    src: {
      fromToken: string;
      fromAmount: bigint;
      bridgeToken: string;
      minBridgeAmount: bigint;
    };
    dst: {
      chainId: string;
      bridgedToken: string;
      toToken: string;
      minToAmount: bigint;
      airdropNativeAmount: bigint;
    };
  }) => {
    if (!account.walletAdapter) {
      throw new Error("walletAdapter is undefined");
    }

    if (!account.address) {
      throw new Error("account.address is undefined");
    }

    if (!inputs.crossChainRouteAddress) {
      throw new Error("crossChainRouteAddress is undefined");
    }

    const { address, src, dst, crossChainRouteAddress } = inputs;
    if (loading) return;
    start();

    try {
      const quotoLZFee = await account.walletAdapter.call(
        crossChainRouteAddress,
        "quoteLayerZeroFee",
        [account.address, dst, dstValutDeposit()],
        {
          abi: woofiDexCrossSwapChainRouterAbi,
        },
      );

      const result = await account.walletAdapter.sendTransaction(
        crossChainRouteAddress,
        "crossSwap",
        {
          from: account.address!,
          to: crossChainRouteAddress,
          data: [account.address, src, dst, dstValutDeposit()],
          value: isNativeTokenChecker(inputs.src.fromToken)
            ? BigInt(inputs.src.fromAmount) + quotoLZFee[0]
            : quotoLZFee[0],
        },
        {
          abi: woofiDexCrossSwapChainRouterAbi,
        },
      );

      stop();

      checkLayerStatus(result.hash);

      ee.emit("deposit:requested");

      // @ts-ignore
      return pick(["from", "to", "hash", "value"], result);
    } catch (e: any) {
      console.error("crossSwap error", e);
      stop();
      throw new Error(e.errorCode);
    }
  };

  return {
    swap,
    loading,
    bridgeStatus: layerStatus,
    status,
    message: bridgeMessage,
  };
};
