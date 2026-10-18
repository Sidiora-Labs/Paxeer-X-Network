import { useCallback, useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import { API, ChainNamespace } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { InputStatus } from "../../../types";
import { CurrentChain } from "../../../types";
import { DEPOSIT_ERROR_CODE_MAP } from "../../../utils";

type Options = {
  sourceToken?: API.TokenInfo;
  targetToken?: API.TokenInfo;
  quantity: string;
  targetQuantity?: string;
  maxQuantity: string;
  isNativeToken: boolean;
  depositFee: bigint;
  depositFeeRevalidating: boolean;
  nativeBalanceRevalidating: boolean;
  dstGasFee: string;
  nativeSymbol?: string;
  nativeBalance?: string;
  account: ReturnType<typeof useAccount>["account"];
  currentChain: CurrentChain | null;
  depositError: string;
  needSwap: boolean;
};

export const useDepositValidation = (options: Options) => {
  const {
    sourceToken,
    targetToken,
    quantity,
    targetQuantity,
    maxQuantity,
    isNativeToken,
    depositFee,
    depositFeeRevalidating,
    nativeBalanceRevalidating,
    dstGasFee,
    nativeSymbol,
    nativeBalance,
    account,
    currentChain,
    depositError,
    needSwap,
  } = options;
  const { t } = useTranslation();

  const insufficientBalance = useMemo(() => {
    if (quantity && Number(quantity) > 0) {
      return new Decimal(quantity).gt(maxQuantity);
    }
    return false;
  }, [quantity, maxQuantity]);

  const insufficientNativeTotal = useMemo(() => {
    if (
      isNativeToken &&
      quantity &&
      Number(quantity) > 0 &&
      !depositFeeRevalidating &&
      !insufficientBalance
    ) {
      const totalNeeded = new Decimal(quantity).add(dstGasFee || 0);
      return totalNeeded.gt(maxQuantity || 0);
    }
    return false;
  }, [
    isNativeToken,
    quantity,
    depositFeeRevalidating,
    insufficientBalance,
    dstGasFee,
    maxQuantity,
  ]);

  const isExceedSourceTokenCap = useMemo(() => {
    if (
      sourceToken?.symbol !== "USDC" &&
      sourceToken?.symbol === targetToken?.symbol &&
      sourceToken?.is_collateral &&
      sourceToken?.user_max_qty !== undefined &&
      sourceToken?.user_max_qty !== -1 &&
      quantity &&
      !isNaN(Number(quantity))
    ) {
      return new Decimal(quantity).gt(sourceToken.user_max_qty);
    }
    return false;
  }, [sourceToken, targetToken, quantity]);

  const isExceedTargetTokenCap = useMemo(() => {
    if (
      targetToken?.symbol !== "USDC" &&
      sourceToken?.symbol !== targetToken?.symbol &&
      targetToken?.is_collateral &&
      targetToken?.user_max_qty !== undefined &&
      targetToken?.user_max_qty !== -1 &&
      targetQuantity &&
      !isNaN(Number(targetQuantity))
    ) {
      // when target quantity exceeds 90% of the max quantity
      return new Decimal(targetQuantity).gte(
        new Decimal(targetToken.user_max_qty).mul(0.9),
      );
    }
    return false;
  }, [targetToken, targetQuantity]);

  const showSourceDepositCap =
    !needSwap &&
    sourceToken?.user_max_qty !== undefined &&
    sourceToken?.user_max_qty !== -1;

  const showTargetDepositCap =
    needSwap &&
    targetToken?.user_max_qty !== undefined &&
    targetToken?.user_max_qty !== -1;

  const depositExceedLimit = useMemo(() => {
    return depositError?.includes(DEPOSIT_ERROR_CODE_MAP.DepositExceedLimit);
  }, [depositError]);

  const insufficientGasMessage = useMemo(() => {
    if (
      nativeSymbol &&
      quantity &&
      Number(quantity) > 0 &&
      // when insufficient balance, the input status is error, so we don't need to check gas balance
      !insufficientBalance &&
      !depositFeeRevalidating &&
      !nativeBalanceRevalidating &&
      (account.walletAdapter?.chainNamespace === ChainNamespace.solana ||
        dstGasFee)
    ) {
      const notEnoughGas = new Decimal(nativeBalance || 0).lt(dstGasFee || 0);

      // when solana, if dstGasFee is 0, and nativeTokenBalance is 0, it means not token balance
      const isNotSolBalance =
        Number(dstGasFee) === 0 && Number(nativeBalance) == Number(dstGasFee);

      if (notEnoughGas || isNotSolBalance) {
        return t("transfer.deposit.notEnoughGas", {
          token: nativeSymbol,
        });
      }
    }
  }, [
    dstGasFee,
    quantity,
    nativeBalanceRevalidating,
    depositFeeRevalidating,
    t,
    nativeSymbol,
    insufficientBalance,
    account,
    nativeBalance,
  ]);

  const gasFeeMessage = useMemo(() => {
    if (insufficientNativeTotal) {
      return t("transfer.deposit.gasFee.error", {
        token: sourceToken?.symbol,
      });
    }
  }, [t, insufficientNativeTotal, sourceToken?.symbol]);

  const globalMaxQtyMessage = useMemo(() => {
    if (
      sourceToken?.symbol !== "USDC" &&
      sourceToken?.symbol === targetToken?.symbol &&
      sourceToken?.is_collateral &&
      depositExceedLimit
    ) {
      return (
        <Trans
          i18nKey="transfer.deposit.globalMaxQty.error"
          values={{
            token: sourceToken?.symbol,
            chain: currentChain?.info?.network_infos?.name || "",
          }}
          components={[
            <a
              key="0"
              href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/multi-collateral#max-deposits-global"
              target="_blank"
              rel="noopener noreferrer"
              className="oui-text-primary"
            />,
          ]}
        />
      );
    }
  }, [
    depositExceedLimit,
    sourceToken?.symbol,
    sourceToken?.is_collateral,
    targetToken?.symbol,
    currentChain,
    t,
  ]);

  const feeWarningMessage = useMemo(() => {
    if (
      quantity &&
      Number(quantity) > 0 &&
      depositFee === 0n &&
      !depositFeeRevalidating &&
      account.walletAdapter?.chainNamespace !== ChainNamespace.solana
    ) {
      return t("transfer.deposit.feeUnavailable");
    }
  }, [t, quantity, depositFee, depositFeeRevalidating, account]);

  const { inputStatus, hintMessage } = useMemo<{
    inputStatus: InputStatus;
    hintMessage: string;
  }>(() => {
    if (!quantity) {
      return { inputStatus: "default", hintMessage: "" };
    }

    if (insufficientBalance) {
      return {
        inputStatus: "error",
        hintMessage: t("transfer.insufficientBalance"),
      };
    }

    if (isExceedSourceTokenCap) {
      return {
        inputStatus: "error",
        hintMessage: t("transfer.deposit.exceedCap"),
      };
    }

    return { inputStatus: "default", hintMessage: "" };
  }, [t, insufficientBalance, isExceedSourceTokenCap]);

  const { targetInputStatus, targetHintMessage } = useMemo<{
    targetInputStatus: InputStatus;
    targetHintMessage: string;
  }>(() => {
    // when target error, set target input status to default
    if (!targetQuantity || inputStatus === "error") {
      return { targetInputStatus: "default", targetHintMessage: "" };
    }

    if (isExceedTargetTokenCap) {
      return {
        targetInputStatus: "error",
        targetHintMessage: t("transfer.deposit.closeToMaxLimit"),
      };
    }

    return { targetInputStatus: "default", targetHintMessage: "" };
  }, [t, targetQuantity, isExceedTargetTokenCap, inputStatus]);

  const slippageValidate = useCallback(
    (value: number) => {
      if (value && value >= 5) {
        return t("transfer.slippage.error.high");
      }
      return "";
    },
    [t],
  );

  const validationMessage =
    globalMaxQtyMessage ||
    gasFeeMessage ||
    feeWarningMessage ||
    insufficientGasMessage;

  const depositDisabled =
    inputStatus === "error" ||
    // if exceed collateral cap, disable deposit button
    isExceedSourceTokenCap ||
    !!feeWarningMessage ||
    !!insufficientNativeTotal ||
    !!insufficientGasMessage;

  return {
    validationMessage,
    hintMessage,
    inputStatus,
    targetInputStatus,
    targetHintMessage,
    depositDisabled,
    showSourceDepositCap,
    showTargetDepositCap,
    slippageValidate,
  };
};
