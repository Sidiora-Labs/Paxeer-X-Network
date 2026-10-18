import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum, NetworkId } from "@paxeer-network/types";
import { Box, Button, ButtonProps } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { DepositAction } from "../../types";

export type ActionButtonProps = {
  disabled?: boolean;
  loading?: boolean;
  actionType: DepositAction;
  symbol?: string;
  onDeposit?: () => void;
  onApprove?: () => void;
  onApproveAndDeposit?: () => void;
  networkId?: NetworkId;
};

export const ActionButton: React.FC<ActionButtonProps> = (props) => {
  const {
    disabled,
    loading,
    actionType,
    symbol = "USDC",
    onDeposit,
    onApprove,
    onApproveAndDeposit,
    networkId,
  } = props;
  const { t } = useTranslation();

  const buttonParams = useMemo(() => {
    const params: Partial<Record<DepositAction, ButtonProps>> = {
      // [DepositAction.Approve]: {
      //   // Approve & Deposit
      //   children: t("transfer.deposit.approve"),
      //   onClick: onApprove,
      //   // approve not disabled button
      //   disabled: false,
      //   "data-testid": "oui-testid-deposit-dialog-approve-btn",
      // },
      [DepositAction.ApproveAndDeposit]: {
        children: `${t("transfer.deposit.approve")} & ${t("common.deposit")}`,
        onClick: onApproveAndDeposit,
        // approve not disabled button
        // disabled: false,
      },
      // [DepositAction.Increase]: {
      //   children: t("transfer.deposit.increase.symbol", { symbol }),
      //   onClick: onApprove,
      //   "data-testid": "oui-testid-deposit-dialog-increase-btn",
      // },
      [DepositAction.Deposit]: {
        children: t("common.deposit"),
        onClick: onDeposit,
        "data-testid": "oui-testid-deposit-dialog-deposit-btn",
      },
    };

    return params[actionType];
  }, [onApprove, onDeposit, actionType, symbol, t]);

  const buttonSize = { initial: "md", lg: "lg" } as const;

  return (
    <Box className="oui-w-full lg:oui-w-auto lg:oui-min-w-[184px]">
      <AuthGuard
        status={AccountStatusEnum.EnableTrading}
        networkId={networkId}
        buttonProps={{
          fullWidth: true,
          size: buttonSize,
        }}
      >
        <Button
          fullWidth
          disabled={disabled}
          loading={loading}
          size={buttonSize}
          {...buttonParams}
        />
      </AuthGuard>
    </Box>
  );
};
