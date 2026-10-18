import { FC, PropsWithChildren, useEffect } from "react";
import { useConfig } from "@paxeer-network/hooks";
import { useSVApiUrl } from "../../hooks/useSVAPIUrl";
import { VaultsPageProps } from "../../pages";
import { useVaultsStore } from "../../store";

export const VaultsProvider: FC<PropsWithChildren<VaultsPageProps>> = (
  props,
) => {
  const svApiUrl = useSVApiUrl();
  const brokerId = useConfig("brokerId");
  const {
    fetchVaultInfo,
    fetchVaultOverallInfo,
    setVaultsPageConfig,
    vaultsPageConfig,
  } = useVaultsStore();

  useEffect(() => {
    if (props.config) {
      setVaultsPageConfig(props.config);
    }
  }, [props.config]);

  useEffect(() => {
    if (!svApiUrl) {
      return;
    }
    fetchVaultInfo(undefined, svApiUrl);
  }, [svApiUrl]);

  // Fetch overall vault info
  useEffect(() => {
    if (!svApiUrl || !brokerId) {
      return;
    }

    // Determine broker_ids parameter
    const brokerIds = vaultsPageConfig?.overallInfoBrokerIds
      ? vaultsPageConfig.overallInfoBrokerIds
      : `paxeer-network,${brokerId}`;

    fetchVaultOverallInfo(
      brokerIds ? { broker_ids: brokerIds } : undefined,
      svApiUrl,
    );
  }, [
    svApiUrl,
    brokerId,
    vaultsPageConfig?.overallInfoBrokerIds,
    fetchVaultOverallInfo,
  ]);

  return <div>{props.children}</div>;
};
