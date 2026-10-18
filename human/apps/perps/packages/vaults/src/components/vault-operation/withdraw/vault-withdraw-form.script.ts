import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Decimal } from "@paxeer-network/utils";
import { useVaultLpInfoById, useVaultsStore } from "../../../store";
import { OperationType } from "../../../types/vault";
import { useOperationScript } from "../depositAndWithdraw/operation.script";

export type VaultWithdrawFormScriptProps = {
  vaultId: string;
};

export const useVaultWithdrawFormScript = (
  props: VaultWithdrawFormScriptProps,
) => {
  const { vaultId } = props;
  const [quantity, setQuantity] = useState<string>("");
  const { handleOperation, disabledOperation } = useOperationScript({
    type: OperationType.WITHDRAWAL,
    vaultId,
  });
  const vaultLpInfo = useVaultLpInfoById(vaultId);
  const { vaultInfo } = useVaultsStore();
  const { t } = useTranslation();

  const currentVault = useMemo(() => {
    return vaultInfo.data.find((v) => v.vault_id === vaultId);
  }, [vaultInfo.data, vaultId]);

  const sharePrice = currentVault?.est_main_share_price || 0;
  const minWithdrawalAmount = currentVault?.min_withdrawal_amount || 0;

  const maxQuantity = vaultLpInfo?.[0]?.available_main_shares || 0;

  const receivingAmount = useMemo(() => {
    if (!quantity || !sharePrice) {
      return "-";
    }
    return new Decimal(quantity).mul(sharePrice).toString();
  }, [quantity, sharePrice]);

  const handleWithdraw = async () => {
    await handleOperation({
      amount: quantity,
      vaultId,
    });
  };

  const onQuantityChange = (value: string) => {
    if (value && new Decimal(value).gt(maxQuantity)) {
      setQuantity(maxQuantity.toString());
    } else {
      setQuantity(value);
    }
  };

  const isMinAmount = useMemo(() => {
    if (!quantity || !sharePrice || !minWithdrawalAmount) return false;
    const isAll = quantity === maxQuantity.toString();

    if (isAll) {
      return false;
    }

    const receiving = new Decimal(quantity).mul(sharePrice);
    return receiving.lt(minWithdrawalAmount);
  }, [quantity, sharePrice, minWithdrawalAmount, maxQuantity]);

  const disabledWithdraw = useMemo(() => {
    return (
      !quantity ||
      quantity === "0" ||
      disabledOperation ||
      (!!quantity && isMinAmount)
    );
  }, [quantity, disabledOperation, isMinAmount]);

  const inputHint = useMemo(() => {
    if (quantity && isMinAmount) {
      return {
        hintMessage: t("vaults.operation.error.minWithdrawal", {
          amount: minWithdrawalAmount,
        }),
        status: "error",
      };
    }
    return {
      hintMessage: "",
      status: "",
    };
  }, [quantity, isMinAmount, minWithdrawalAmount, t]);

  return {
    quantity,
    onQuantityChange,
    maxQuantity,
    handleWithdraw,
    vaultId,
    sharePrice,
    receivingAmount,
    disabledWithdraw,
    disabledOperation,
    inputHint,
  };
};

export type VaultWithdrawFormScript = ReturnType<
  typeof useVaultWithdrawFormScript
>;
