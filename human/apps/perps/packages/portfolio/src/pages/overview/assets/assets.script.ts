import { useCallback, useMemo } from "react";
import {
  useAccount,
  useCollateral,
  useLeverage,
  useLocalStorage,
  usePositionStream,
  useWalletConnector,
  useMaxWithdrawal,
} from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { modal, useScreen } from "@paxeer-network/ui";
import { LeverageWidgetWithDialogId } from "@paxeer-network/ui-leverage";
import {
  DepositAndWithdrawWithDialogId,
  DepositAndWithdrawWithSheetId,
  TransferDialogId,
  TransferSheetId,
} from "@paxeer-network/ui-transfer";

export const useAssetScript = () => {
  const { connect, namespace } = useWalletConnector();
  const { state, isMainAccount } = useAccount();
  const { totalValue, freeCollateral } = useCollateral();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const [data] = usePositionStream();
  const { curLeverage } = useLeverage();
  const [visible, setVisible] = useLocalStorage("paxeerNetwork_assets_visible", true);
  const { isMobile } = useScreen();
  const handleDomId = isMobile
    ? DepositAndWithdrawWithSheetId
    : DepositAndWithdrawWithDialogId;
  const subAccounts = state.subAccounts ?? [];

  const canTrade = useMemo(() => {
    return (
      !wrongNetwork &&
      !disabledConnect &&
      (state.status === AccountStatusEnum.EnableTrading ||
        state.status === AccountStatusEnum.EnableTradingWithoutConnected)
    );
  }, [state.status, wrongNetwork, disabledConnect]);

  const onLeverageEdit = () => {
    modal.show(LeverageWidgetWithDialogId);
  };

  const onDeposit = useCallback(() => {
    modal.show(handleDomId, { activeTab: "deposit" });
  }, [handleDomId]);

  const onWithdraw = useCallback(() => {
    modal.show(handleDomId, { activeTab: "withdraw" });
  }, [handleDomId]);

  const onTransfer = useCallback(() => {
    if (isMobile) {
      modal.show(TransferSheetId);
    } else {
      modal.show(TransferDialogId);
    }
  }, [isMobile]);

  const maxWithdrawAmount = useMaxWithdrawal("USDC");

  return {
    canTrade,
    connect,
    portfolioValue: totalValue,
    freeCollateral,
    maxWithdrawAmount,
    unrealPnL: data.aggregated.total_unreal_pnl,
    unrealROI: data.totalUnrealizedROI,
    currentLeverage: curLeverage,
    onLeverageEdit,
    visible,
    wrongNetwork,
    toggleVisible: () => setVisible(!visible),
    onDeposit,
    onWithdraw,
    onTransfer,
    namespace,
    isMainAccount,
    hasSubAccount: subAccounts?.length > 0,
  } as const;
};

export type AssetScriptReturn = ReturnType<typeof useAssetScript>;
