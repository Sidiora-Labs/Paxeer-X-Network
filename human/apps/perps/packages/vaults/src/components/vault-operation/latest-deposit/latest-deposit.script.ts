import { OperationType } from "../../../types/vault";
import { useLatestOperationScript } from "../depositAndWithdraw/latest.script";

export const useLatestDepositScript = ({ vaultId }: { vaultId: string }) => {
  const { latestOperation } = useLatestOperationScript({
    type: OperationType.DEPOSIT,
    vaultId,
  });
  return {
    latestOperation,
  };
};

export type LatestDepositScript = ReturnType<typeof useLatestDepositScript>;
