import { useMemo, useState } from "react";
import {
  useAccount,
  useChains,
  useConfig,
  useMutation,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { AccountStatusEnum, ChainNamespace } from "@paxeer-network/types";
import { modal, toast } from "@paxeer-network/ui";

export function useFaucetScript() {
  const { t } = useTranslation();
  const { connectedChain, namespace } = useWalletConnector();
  const [, { isTestnetChain }] = useChains();
  const { state, account } = useAccount();
  const config = useConfig();
  const operatorUrl = config.get<string>("operatorUrl");

  const [getTestUSDC, { isMutating }] = useMutation(
    `${operatorUrl}/v1/faucet/usdc`,
  );
  const [loading, setLoading] = useState<boolean>(false);

  const showFaucet = useMemo(() => {
    if (!connectedChain || !connectedChain.id) {
      return false;
    }
    return (
      (state.status === AccountStatusEnum.EnableTrading ||
        state.status === AccountStatusEnum.EnableTradingWithoutConnected) &&
      isTestnetChain(connectedChain.id)
    );
  }, [state, connectedChain, isTestnetChain]);

  const getFaucet = () => {
    if (loading) {
      return;
    }
    setLoading(true);
    const message = t("trading.faucet.getTestUSDC.success", {
      quantity: namespace === ChainNamespace.solana ? "100" : "1,000",
    });

    return getTestUSDC({
      chain_id: account.walletAdapter?.chainId.toString(),
      user_address: state.address,
      broker_id: config.get("brokerId"),
    }).then(
      (res) => {
        setLoading(false);
        if (res.success) {
          return modal.alert({
            title: t("trading.faucet.getTestUSDC"),
            message,
            onOk: () => {
              return new Promise((resolve) => resolve(true));
            },
          });
        }
        res.message && toast.error(res.message);
      },
      (error: Error) => {
        toast.error(error.message);
      },
    );
  };
  return { getFaucet, showFaucet, loading };
}

export type FaucetState = ReturnType<typeof useFaucetScript>;
