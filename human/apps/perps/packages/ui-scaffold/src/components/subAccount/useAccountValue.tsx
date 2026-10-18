import { useEffect, useMemo, useState } from "react";
import { useSubAccountQuery } from "@paxeer-network/hooks";
import { API } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";

export const useAccountValue = (mainAccountId?: string) => {
  const [accountValue, setAccountValue] = useState<Record<string, number>>({});
  const {
    data: newPositions = [],
    isLoading: isPositionLoading,
    mutate: mutatePositions,
  } = useSubAccountQuery<API.PositionExt[]>("/v1/client/aggregate/positions", {
    // formatter: (data) => data,
    errorRetryCount: 3,
    accountId: mainAccountId,
  });

  useEffect(() => {
    if (isPositionLoading) {
      return;
    }
    if (!newPositions || newPositions.length === 0) {
      setAccountValue({});
      return;
    }
    const value = newPositions.reduce(
      (acc, position) => {
        const accountId = position.account_id!;
        if (acc[accountId]) {
          acc[accountId] = new Decimal(acc[accountId])
            .plus(position.unsettled_pnl)
            .toNumber();
        } else {
          acc[accountId] = new Decimal(position.unsettled_pnl).toNumber();
        }
        return acc;
      },
      {} as Record<string, number>,
    );
    setAccountValue(value);
  }, [newPositions, isPositionLoading]);
  return {
    accountValue,
  };
};
