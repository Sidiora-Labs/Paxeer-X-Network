import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum, NetworkId } from "@paxeer-network/types";
import { Box, Button, modal } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { Decimal } from "@paxeer-network/utils";
import { WithdrawTo } from "../../types";
import { CrossWithdrawConfirm } from "../crossWithdrawConfirm";
import SwitchChainButton from "./SwitchChainButton";

interface IProps {
  disabled?: boolean;
  loading?: boolean;
  onWithdraw: () => Promise<void>;
  networkId?: NetworkId;
  crossChainWithdraw: boolean;
  address?: string;
  currentChain?: any;
  quantity: string;
  fee: number;
  checkIsBridgeless: boolean;
  withdrawTo: WithdrawTo;
  onTransfer: () => void;
}

export const WithdrawAction = (props: IProps) => {
  const {
    disabled,
    loading,
    onWithdraw,
    networkId,
    crossChainWithdraw,
    address,
    currentChain,
    quantity,
    fee,
    checkIsBridgeless,
    onTransfer,
  } = props;
  const { t } = useTranslation();

  const amount = useMemo(() => {
    if (!quantity) {
      return 0;
    }
    return new Decimal(quantity).minus(fee ?? 0).toNumber();
  }, [quantity, fee]);

  const preWithdraw = () => {
    if (crossChainWithdraw) {
      modal.confirm({
        title: t("transfer.withdraw.crossChain.confirmWithdraw"),
        content: (
          <CrossWithdrawConfirm
            address={address!}
            amount={amount}
            currentChain={currentChain}
          />
        ),
        classNames: {
          content: "oui-font-semibold",
          body: "!oui-pb-0",
          footer: "!oui-pt-0",
        },

        onOk: async () => {
          onWithdraw();
        },
      });
      return;
    }
    onWithdraw();
  };

  const buttonSize = { initial: "md", lg: "lg" } as const;

  const renderButton = () => {
    if (props.withdrawTo === WithdrawTo.Account) {
      return (
        <Button
          fullWidth
          disabled={disabled}
          loading={loading}
          onClick={onTransfer}
          size={buttonSize}
        >
          {t("common.withdraw")}
        </Button>
      );
    }

    if (checkIsBridgeless) {
      return (
        <Button
          data-testid="oui-testid-withdraw-dialog-withdraw-btn"
          fullWidth
          disabled={disabled}
          loading={loading}
          onClick={preWithdraw}
          size={buttonSize}
        >
          {t("common.withdraw")}
        </Button>
      );
    }
    return <SwitchChainButton networkId={networkId} size={buttonSize} />;
  };

  return (
    <Box className="oui-w-full lg:oui-w-auto lg:oui-min-w-[184px]">
      <AuthGuard
        status={AccountStatusEnum.EnableTrading}
        networkId={networkId}
        bridgeLessOnly
        buttonProps={{ fullWidth: true, size: buttonSize }}
      >
        {renderButton()}
      </AuthGuard>
    </Box>
  );
};
