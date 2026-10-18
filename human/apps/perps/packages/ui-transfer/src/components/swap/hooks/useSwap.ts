import { useCallback, useEffect, useRef, useState } from "react";
import { pick } from "ramda";
import { utils } from "@paxeer-network/core";
import {
  useAccountInstance,
  useBoolean,
  useConfig,
  useEventEmitter,
} from "@paxeer-network/hooks";
import { WS_WalletStatusEnum } from "@paxeer-network/types";
import { isNativeTokenChecker, woofiDexSwapDepositorAbi } from "../constants";

/**
 * 1. fee precision swap_support.woofi_dex_precision+3，round off
 * 2. price precision = abs(woofi_dex_precision - 5)，cut off
 * 3. paxeer-network deposit fee = $0
 * 4. deposit pop-ups: don't show token when fee is 0.
 * e.g. dst gas fee = 0 ETH, swap fee = 0.04 USDC, it will show $0.04 ( 0.04 USDC )
 */
export const useSwap = (): any => {
  const [loading, { setTrue: start, setFalse: stop }] = useBoolean(false);
  const account = useAccountInstance();
  const brokerId = useConfig("brokerId");

  const [status, setStatus] = useState<WS_WalletStatusEnum>(
    WS_WalletStatusEnum.NO,
  );

  const txHash = useRef<string | undefined>();
  const ee = useEventEmitter();

  useEffect(() => {
    const handler = (data: any) => {
      const { side, transStatus, trxId } = data;

      if (side === "DEPOSIT" && trxId === txHash.current) {
        setStatus(transStatus);
      }
    };
    ee.on("wallet:changed", handler);

    return () => {
      ee.off("wallet:changed", handler);
    };
  }, [txHash.current]);

  const dstValutDeposit = useCallback(() => {
    return {
      accountId: account.accountIdHashStr,
      brokerHash: utils.parseBrokerHash(brokerId),
      tokenHash: utils.parseTokenHash("USDC"),
    };
  }, [account, brokerId]);

  const swap = useCallback(
    async (
      woofiDexDepositorAdress: string,
      inputs: {
        fromToken: string;
        fromAmount: string;
        toToken: string;
        minToAmount: string;
        paxeerNetworkNativeFees: bigint;
      },
      config: { dst: any; src: any },
    ) => {
      if (!account.walletAdapter) {
        throw new Error("walletAdapter is undefined");
      }

      if (!account.address) {
        throw new Error("account.address is undefined");
      }

      if (loading) return;
      start();

      const txPayload = {
        from: account.address,
        to: woofiDexDepositorAdress,
        data: [account.address, inputs, dstValutDeposit()],
        value: isNativeTokenChecker(inputs.fromToken)
          ? BigInt(inputs.fromAmount) + inputs.paxeerNetworkNativeFees
          : inputs.paxeerNetworkNativeFees,
      };

      try {
        const result = await account.walletAdapter.sendTransaction(
          woofiDexDepositorAdress,
          "swap",
          txPayload,
          {
            abi: woofiDexSwapDepositorAbi,
          },
        );

        stop();

        txHash.current = result.hash;

        ee.emit("deposit:requested");

        // @ts-ignore
        return pick(["from", "to", "hash", "value"], result);
      } catch (e: any) {
        throw new Error(e.errorCode);
      }
    },
    [loading, account],
  );

  return {
    swap,
    loading,
    status,
  };
};
