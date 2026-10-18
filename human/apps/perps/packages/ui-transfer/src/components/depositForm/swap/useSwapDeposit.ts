import { useCallback } from "react";
import { utils } from "@paxeer-network/core";
import { useAccount, useConfig } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, isNativeTokenChecker } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { CurrentChain } from "../../../types";
import swapContractAbi from "./perp_vault_depositor.json";
import { useSlippage } from "./useSlippage";
import { useSwapQuote } from "./useSwapQuote";

type SwapDepositOptions = {
  sourceToken?: API.TokenInfo;
  targetToken?: API.TokenInfo;
  currentChain?: CurrentChain | null;
  quantity: string;
  depositFee: bigint;
};

export const SWAP_CONTRACT_ADDRESS =
  "0xe18f1051d1cCeCb61572eb686e1beb34A8612bf3";

export const useSwapDeposit = (options: SwapDepositOptions) => {
  const { sourceToken, targetToken, currentChain, quantity, depositFee } =
    options;
  const { account } = useAccount();
  const brokerId = useConfig("brokerId") as string;

  const { slippage, onSlippageChange } = useSlippage();

  const { t } = useTranslation();

  const {
    swapData,
    swapPrice,
    swapQuantity,
    swapMinReceived,
    swapPriceRevalidating,
    error,
  } = useSwapQuote({
    sourceToken,
    targetToken,
    quantity,
    chainId: currentChain?.id,
    slippage,
  });

  const onSwapDeposit = useCallback(async () => {
    if (
      !account.address ||
      !account.walletAdapter ||
      !swapData ||
      !sourceToken ||
      !targetToken ||
      !brokerId
    ) {
      return;
    }

    const userAddress = account.address;

    const receiver = userAddress;
    const paxeerNetworkBridgeFee = depositFee;

    const info = {
      fromToken: swapData.infos.from_token,
      bridgeToken: swapData.infos.to_token,
      fromAmount: swapData.infos.from_amount,
      minBridgeAmount: swapData.infos.min_to_amount,
    };

    const extSwapInfo = {
      swapRouter: swapData["1inch"].swap_router,
      data: swapData["1inch"].data,
    };

    const perpAccountInfo = {
      accountId: account.accountIdHashStr,
      brokerHash: utils.parseBrokerHash(brokerId),
      tokenHash: utils.parseTokenHash(targetToken.symbol!),
    };

    const value = isNativeTokenChecker(sourceToken.address!)
      ? BigInt(swapData.infos.from_amount) + paxeerNetworkBridgeFee
      : paxeerNetworkBridgeFee;

    const payload = {
      from: userAddress,
      to: SWAP_CONTRACT_ADDRESS,
      data: [receiver, paxeerNetworkBridgeFee, info, extSwapInfo, perpAccountInfo],
      value,
    };

    console.info("swap deposit payload", payload);

    try {
      const result = await account.walletAdapter.sendTransaction(
        SWAP_CONTRACT_ADDRESS,
        "deposit",
        payload,
        {
          abi: swapContractAbi,
        },
      );

      console.info("swap deposit result", result);

      return result;
    } catch (err: any) {
      console.error("swap deposit error", err);
      toast.error(err.message || t("common.somethingWentWrong"));
      throw err;
    }
  }, [account, brokerId, depositFee, sourceToken, swapData, t, targetToken]);

  return {
    swapPrice,
    swapQuantity,
    swapMinReceived,
    swapPriceRevalidating,
    slippage,
    onSlippageChange,
    onSwapDeposit,
    error,
  };
};
