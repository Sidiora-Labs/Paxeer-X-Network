import { useCallback, useMemo } from "react";
import {
  useAccountInstance,
  useEventEmitter,
  useLocalStorage,
  useAccount,
  useConfig,
  useCollateral,
  useMarginRatio,
  usePositionStream,
  useComputedLTV,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum, NetworkId } from "@paxeer-network/types";
import { modal, toast } from "@paxeer-network/ui";
import {
  DepositAndWithdrawWithDialogId,
  TransferDialogId,
} from "@paxeer-network/ui-transfer";

export const useAssetViewScript = () => {
  const { t } = useTranslation();
  const account = useAccountInstance();
  const ee = useEventEmitter();

  const { totalValue } = useCollateral({
    dp: 2,
  });

  const networkId = useConfig("networkId") as NetworkId;
  const { state, isMainAccount } = useAccount();
  const { freeCollateral } = useCollateral({
    dp: 2,
  });
  const { marginRatio, mmr, maintenanceMargin, currentLeverage } =
    useMarginRatio();
  const isConnected = state.status >= AccountStatusEnum.Connected;
  const [{ aggregated }, positionsInfo] = usePositionStream();
  const marginRatioVal = useMemo(() => {
    return Math.min(
      10,
      aggregated.notional === 0
        ? positionsInfo["margin_ratio"](10)!
        : marginRatio,
    );
  }, [marginRatio, aggregated]);

  // const renderMMR = useMemo(() => {
  //   if (!mmr) {
  //     return "";
  //   }
  //   const bigMMR = new Decimal(mmr);
  //   return bigMMR.mul(100).todp(2, 0).toFixed(2);
  // }, [mmr]);

  const openDepositAndWithdraw = useCallback(
    (viewName: "deposit" | "withdraw") => {
      // desktop always show dialog
      return modal.show(DepositAndWithdrawWithDialogId, {
        activeTab: viewName,
      });
    },
    [],
  );

  const onDeposit = useCallback(async () => {
    return openDepositAndWithdraw("deposit");
  }, []);

  const onWithdraw = useCallback(async () => {
    return openDepositAndWithdraw("withdraw");
  }, []);

  const onTransfer = useCallback(async () => {
    return modal.show(TransferDialogId);
  }, []);

  const onSettle = useCallback(async () => {
    return account
      .settle()
      .catch((e) => {
        if (e.code === -1104) {
          toast.error(t("settle.settlement.error"));
          return Promise.reject(e);
        }
        if (
          e.message.indexOf(
            "Signing off chain messages with Ledger is not yet supported",
          ) !== -1
        ) {
          ee.emit("wallet:sign-message-with-ledger-error", {
            message: e.message,
            userAddress: account.address,
          });
        }
      })
      .then((res) => {
        toast.success(t("settle.settlement.requested"));
        return Promise.resolve(res);
      });
  }, [account, t]);

  const [visible, setVisible] = useLocalStorage<boolean>(
    "paxeerNetwork_assets_visible",
    true,
  );

  const toggleVisible = useCallback(() => {
    // @ts-ignore
    setVisible((visible: boolean) => {
      return !visible;
    });
  }, [visible]);

  const currentLtv = useComputedLTV();
  const _freeCollateral = useDataTap(freeCollateral) ?? undefined;
  const _marginRatioVal = useDataTap(marginRatioVal) ?? undefined;
  const _mmr = useDataTap(mmr) ?? undefined;
  const _maintenanceMargin = useDataTap(maintenanceMargin) ?? undefined;
  const _currentLeverage = useDataTap(currentLeverage) ?? undefined;
  const _totalValue = useDataTap(totalValue) ?? undefined;

  return {
    onDeposit,
    onWithdraw,
    onTransfer,
    onSettle,
    visible,
    toggleVisible,
    networkId,
    totalValue: _totalValue,
    status: state.status,
    freeCollateral: _freeCollateral,
    marginRatioVal: _marginRatioVal,
    renderMMR: _mmr,
    maintenanceMargin: _maintenanceMargin,
    currentLeverage: _currentLeverage,
    isConnected,
    isMainAccount,
    hasSubAccount: !!state.subAccounts?.length,
    currentLtv,
  };
};

export type AssetViewState = ReturnType<typeof useAssetViewScript>;
