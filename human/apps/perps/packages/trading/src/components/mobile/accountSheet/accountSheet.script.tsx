import { useMemo } from "react";
import {
  TWType,
  useAccount,
  useChains,
  useConfig,
  useCurEpochEstimate,
  useEpochInfo,
  useLocalStorage,
  useMutation,
  useReferralInfo,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum, ChainNamespace } from "@paxeer-network/types";
import { modal, toast, useModal } from "@paxeer-network/ui";
import { ReferralProps, TradingRewardsProps } from "../../../types/types";

export const useAccountSheetScript = (
  props: ReferralProps & TradingRewardsProps,
) => {
  const { t } = useTranslation();
  const [linkDeviceStorage] = useLocalStorage("paxeerNetwork_link_device", {});
  const { account, state } = useAccount();
  const accountId = account.accountId;
  const address = account.address;
  const { hide } = useModal();

  const config = useConfig();

  const { connectedChain, disconnect, namespace } = useWalletConnector();
  const [, { isTestnetChain }] = useChains();

  const chainId =
    account.chainId || connectedChain?.id || linkDeviceStorage?.chainId;

  const showGetTestUSDC = useMemo(() => {
    if (chainId) {
      return (
        (state.status === AccountStatusEnum.EnableTrading ||
          state.status === AccountStatusEnum.EnableTradingWithoutConnected) &&
        isTestnetChain(chainId)
      );
    }

    return false;
  }, [state.status, chainId, isTestnetChain]);

  const chainName = useGetChains(chainId);

  const operatorUrl = config.get<string>("operatorUrl");

  const onCopyAddress = () => {
    navigator.clipboard.writeText(address ?? "");
    toast.success(t("common.copy.copied"));
  };

  const {
    affiliateCommission30D,
    traderCommission30D,
    isAffiliate,
    isTrader,
    onClickReferral,
  } = useReferral(props.onClickReferral);

  const { curEpochId, estRewards, onClickTradingRewards } = useTradingRewards(
    props.onClickTradingRewards,
  );

  const onDisconnect = async () => {
    // The cache must be cleared first, otherwise it will be possible entered link device mode
    localStorage.removeItem("paxeerNetwork_link_device");
    await disconnect({
      label: state.connectWallet?.name,
    });
    await account.disconnect();
    hide();
  };

  const [getTestUSDC, { isMutating: gettingTestUSDC }] = useMutation(
    `${operatorUrl}/v1/faucet/usdc`,
  );

  const onGetTestUSDC = () => {
    if (
      state.status < AccountStatusEnum.EnableTrading &&
      state.status !== AccountStatusEnum.EnableTradingWithoutConnected
    ) {
      // return modal.show(WalletConnectSheet, {
      //   status: state.status,
      // });
      return;
    }

    const message = t("trading.faucet.getTestUSDC.success", {
      quantity: namespace === ChainNamespace.solana ? "100" : "1,000",
    });

    return getTestUSDC({
      chain_id: chainId?.toString(),
      user_address: state.address,
      broker_id: config.get("brokerId"),
    }).then(
      (res: any) => {
        if (res.success) {
          return modal.alert({
            title: t("trading.faucet.getTestUSDC"),
            message,
            onOk: () => {
              return Promise.resolve(true);
            },
          });
        }
        res.message && toast.error(res.message);
        // return Promise.reject(res);
      },
      (error: Error) => {
        toast.error(error.message);
      },
    );
  };
  return {
    accountId,
    address,
    chainId,
    chainName,
    onCopyAddress,

    affiliateCommission30D,
    onClickReferral,
    isAffiliate,
    isTrader,
    traderCommission30D,

    curEpochId,
    onClickTradingRewards,
    estRewards,

    onDisconnect,
    showGetTestUSDC,
    onGetTestUSDC,
    gettingTestUSDC,
  };
};

const useReferral = (_onClickReferral?: () => void) => {
  const { data, isLoading, isAffiliate, isTrader } = useReferralInfo();

  const affiliateCommission30D = useMemo(() => {
    if (isAffiliate) {
      return data?.referrer_info?.["30d_referrer_rebate"];
    }
    return undefined;
  }, [data, isAffiliate]);

  const traderCommission30D = useMemo(() => {
    if (isTrader) {
      return data?.referee_info?.["30d_referee_rebate"];
    }
    return undefined;
  }, [data, isTrader]);

  const onClickReferral = () => {
    _onClickReferral?.();
  };

  return {
    onClickReferral,
    affiliateCommission30D,
    traderCommission30D,
    isAffiliate,
    isTrader,

    // WARNING: test data
    // affiliateCommission30D: 1234.43,
    // traderCommission30D:44343.33,
    // isAffiliate: true,
    // isTrader: true,
  };
};

const useTradingRewards = (_onClick?: () => void) => {
  const [curEpochEstimate] = useCurEpochEstimate(TWType.normal);
  const [list, curEpoch] = useEpochInfo(TWType.normal);
  const curEpochId = useMemo(() => {
    return curEpoch.curEpochInfo?.epoch_id ?? "--";
  }, [curEpoch]);

  const estRewards = useMemo(() => {
    if (typeof curEpochEstimate?.est_r_wallet === "undefined") {
      return "--";
    }
    return curEpochEstimate?.est_r_wallet;
  }, [curEpochEstimate]);

  const onClickTradingRewards = () => {
    _onClick?.();
  };

  return {
    curEpochId,
    estRewards,
    onClickTradingRewards,
  };
};

function useGetChains(chainId: number) {
  const [mainChains, { findByChainId }] = useChains("mainnet", {
    pick: "network_infos",
    filter: (chain: any) =>
      chain.network_infos?.bridge_enable || chain.network_infos?.bridgeless,
  });

  const chainName = useMemo(() => {
    // @ts-ignore
    const chain = findByChainId(parseInt(chainId), "network_infos");

    if (!chain) {
      return "Unknown";
    }
    // // @ts-ignore
    // if (isTestnet(chain.chain_id)) {
    //   return "Testnet";
    // }
    // @ts-ignore
    return chain.name;
  }, [chainId, findByChainId]);

  return chainName;
}

export type AccountSheetState = ReturnType<typeof useAccountSheetScript>;
