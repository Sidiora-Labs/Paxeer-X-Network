import { useCallback, useMemo } from "react";
import {
  useChains,
  useConfig,
  useLocalStorage,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  API,
  isWalletChainChangePendingResult,
  NetworkId,
} from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { int2hex, praseChainIdToNumber } from "@paxeer-network/utils";
import { CurrentChain } from "../../../types";

export function useChainSelect() {
  const { t } = useTranslation();
  const networkId = useConfig("networkId") as NetworkId;
  const [linkDeviceStorage] = useLocalStorage("paxeerNetwork_link_device", {});

  const { connectedChain, settingChain, setChain } = useWalletConnector();

  const [chains, { findByChainId }] = useChains(networkId, {
    pick: "network_infos",
    filter: (chain: any) =>
      chain.network_infos?.bridge_enable || chain.network_infos?.bridgeless,
  });

  const currentChain = useMemo(() => {
    const chainId = connectedChain
      ? praseChainIdToNumber(connectedChain.id)
      : parseInt(linkDeviceStorage?.chainId);

    if (!chainId) return null;

    const chain = findByChainId(chainId);

    return {
      ...connectedChain,
      id: chainId,
      info: chain!,
    } as CurrentChain;
  }, [findByChainId, connectedChain, linkDeviceStorage]);

  const onChainChange = useCallback(
    async (chain: API.NetworkInfos) => {
      const chainInfo = findByChainId(chain.chain_id);

      if (!connectedChain) {
        return;
      }

      if (
        !chainInfo ||
        chainInfo.network_infos?.chain_id === currentChain?.id
      ) {
        return Promise.resolve();
      }

      return setChain({
        chainId: int2hex(Number(chainInfo.network_infos?.chain_id)),
      })
        .then((switched) => {
          if (isWalletChainChangePendingResult(switched)) {
            return;
          }
          if (switched) {
            toast.success(t("connector.networkSwitched"));
          } else {
            toast.error(t("connector.switchChain.failed"));
          }
        })
        .catch((error) => {
          if (error && error.message) {
            toast.error(
              `${t("connector.switchChain.failed")}: ${error.message}`,
            );
          }
        });
    },
    [connectedChain, currentChain, findByChainId, setChain, t],
  );

  return {
    chains,
    currentChain,
    settingChain,
    onChainChange,
  };
}
