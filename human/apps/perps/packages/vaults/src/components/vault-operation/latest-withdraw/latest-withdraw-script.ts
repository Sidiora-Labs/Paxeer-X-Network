import { OperationType } from "../../../types/vault";
import { useLatestOperationScript } from "../depositAndWithdraw/latest.script";

export const useLatestWithdrawScript = ({ vaultId }: { vaultId: string }) => {
  const { latestOperation } = useLatestOperationScript({
    type: OperationType.WITHDRAWAL,
    vaultId,
  });
  return {
    latestOperation,
  };
};

export type LatestWithdrawScript = ReturnType<typeof useLatestWithdrawScript>;
