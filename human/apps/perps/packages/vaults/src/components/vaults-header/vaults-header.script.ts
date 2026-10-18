import { useMemo } from "react";
import { uniqBy } from "ramda";
import { useConfig } from "@paxeer-network/hooks";
import { useVaultsStore } from "../../store";
import { VaultSupportedChain } from "../../types/vault";

export const useVaultsHeaderScript = () => {
  const { vaultInfo, vaultsPageConfig } = useVaultsStore();
  const brokerName = useConfig("brokerName");

  const supportVaults = useMemo(() => {
    const array: VaultSupportedChain[] = [];
    vaultInfo.data.forEach((vault) => {
      // Add defensive check to prevent crash when supported_chains is undefined
      if (Array.isArray(vault?.supported_chains)) {
        array.push(...vault.supported_chains);
      }
    });

    return uniqBy((item) => item.chain_id, array);
  }, [vaultInfo.data]);

  return {
    supportVaults,
    headerImage: vaultsPageConfig?.headerImage,
    brokerName,
  };
};

export type VaultsHeaderScript = ReturnType<typeof useVaultsHeaderScript>;
