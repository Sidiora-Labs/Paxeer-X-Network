import { useMemo } from "react";
import { useQuery } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { WithdrawTo } from "../../../types";
import { CurrentChain } from "../../depositForm/hooks";

// CCTP (Cross-Chain Transfer Protocol) Supported AVAX, ETH, POL, ARB, OP, BASE, SEI
const cctpSupportedChains = [43114, 1, 137, 42161, 10, 8453, 1329];
// CCTP Supported Tokens
const cctpSupportedTokens = ["USDC"];

export const useVaultBalance = (inputs: {
  currentChain?: CurrentChain | null;
  sourceToken?: API.TokenInfo;
  withdrawTo: WithdrawTo;
  quantity: string;
  qtyGreaterThanMaxAmount: boolean;
}) => {
  const {
    currentChain,
    sourceToken,
    withdrawTo,
    quantity,
    qtyGreaterThanMaxAmount,
  } = inputs;

  const { t } = useTranslation();

  const { data: vaultBalanceData } = useQuery<API.VaultBalance[]>(
    `/v1/public/vault_balance`,
    {
      revalidateOnMount: true,
      errorRetryCount: 3,
    },
  );

  const vaultBalanceList = useMemo(() => {
    if (
      withdrawTo === WithdrawTo.Account ||
      !currentChain ||
      !Array.isArray(vaultBalanceData)
    ) {
      return [];
    }
    return vaultBalanceData.filter(
      (item) => Number.parseInt(item.chain_id) === currentChain?.id,
    );
  }, [vaultBalanceData, currentChain, withdrawTo]);

  const chainVaultBalance = useMemo(() => {
    const vaultBalance = vaultBalanceList?.find(
      (item) => item.token === sourceToken?.symbol,
    );
    return vaultBalance?.balance;
  }, [vaultBalanceList, sourceToken]);

  const qtyGreaterThanVault = useMemo<boolean>(() => {
    if (
      !quantity ||
      Number.isNaN(quantity) ||
      !chainVaultBalance ||
      Number.isNaN(chainVaultBalance)
    ) {
      return false;
    }

    return new Decimal(quantity).gt(chainVaultBalance);
  }, [quantity, chainVaultBalance, withdrawTo]);

  const crossChainWithdraw = useMemo(() => {
    const canCCTP =
      currentChain?.id && sourceToken?.symbol
        ? cctpSupportedChains.includes(currentChain?.id) &&
          cctpSupportedTokens.includes(sourceToken?.symbol)
        : false;

    return qtyGreaterThanVault && canCCTP && !qtyGreaterThanMaxAmount;
  }, [qtyGreaterThanVault, qtyGreaterThanMaxAmount, currentChain, sourceToken]);

  const chainName = useMemo(() => {
    if (currentChain && currentChain.info && currentChain.info.network_infos) {
      return currentChain.info.network_infos.name;
    }
    return "";
  }, [currentChain]);

  const vaultBalanceMessage = useMemo(() => {
    if (qtyGreaterThanVault && !crossChainWithdraw && chainName) {
      return t("transfer.withdraw.vaultWarning", {
        tokenName: sourceToken?.symbol!,
        chainName: chainName,
        balance: chainVaultBalance,
      });
    }
    return "";
  }, [
    qtyGreaterThanVault,
    crossChainWithdraw,
    chainName,
    sourceToken,
    chainVaultBalance,
    t,
  ]);

  const vaultBalanceListFilterCCTP = useMemo(() => {
    if (cctpSupportedChains.includes(currentChain?.id!)) {
      return vaultBalanceList?.filter(
        (item) => !cctpSupportedTokens.includes(item.token),
      );
    }
    return vaultBalanceList;
  }, [vaultBalanceList, currentChain]);

  return {
    vaultBalanceList: vaultBalanceListFilterCCTP,
    // chainVaultBalance,
    // if the token is cross chain withdraw supported, the qtyGreaterThanVault will be false
    qtyGreaterThanVault: qtyGreaterThanVault && !crossChainWithdraw,
    crossChainWithdraw,
    vaultBalanceMessage,
  };
};
