import { useMemo } from "react";
import { useVaultOverallInfoState } from "../../store";

export const useVaultsIntroductionScript = () => {
  const { data: overallInfo, loading, error } = useVaultOverallInfoState();

  const vaultsInfo = useMemo(() => {
    if (!overallInfo) {
      return {
        tvl: 0,
        lpCount: 0,
        vaultsCount: 0,
      };
    }

    return {
      tvl: overallInfo.strategy_vaults_tvl,
      lpCount: overallInfo.strategy_vaults_lp_count,
      vaultsCount: overallInfo.strategy_vaults_count,
    };
  }, [overallInfo]);

  return {
    vaultsInfo,
    loading,
    error,
  };
};

export type VaultsIntroductionScript = ReturnType<
  typeof useVaultsIntroductionScript
>;
