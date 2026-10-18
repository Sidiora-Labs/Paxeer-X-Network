import { useMemo } from "react";
import { usePrivateQuery } from "@paxeer-network/hooks";
import { EMPTY_LIST } from "@paxeer-network/types";
import { OperationType, VaultOperation } from "../../../types/vault";

type LatestOperationScriptProps = {
  type: OperationType;
  vaultId: string;
};

export const useLatestOperationScript = (props: LatestOperationScriptProps) => {
  const { type, vaultId } = props;
  const { data: operationHistory, mutate: mutateOperationHistory } =
    usePrivateQuery<VaultOperation[]>(
      `/v1/account_sv_transaction_history?type=${type}&vault_id=${vaultId}&size=1`,
      {
        formatter: (response: { rows: VaultOperation[] }) => {
          return response?.rows || EMPTY_LIST;
        },
        revalidateOnFocus: false,
      },
    );

  const latestOperation = useMemo(() => {
    return operationHistory?.[0];
  }, [operationHistory]);

  return {
    latestOperation,
    refetch: mutateOperationHistory,
  };
};

export type LatestOperationScript = ReturnType<typeof useLatestOperationScript>;
