import { useCallback, useMemo } from "react";
import { useMutation } from "../useMutation";
import { usePrivateQuery } from "../usePrivateQuery";
import { useQuery } from "../useQuery";

const generateLeverageLevers = (max: number) => {
  const min = 1;
  const parts = 5;
  const step = (max - min) / (parts - 1);
  const result: number[] = [];
  for (let i = 0; i < parts; i++) {
    result.push(Math.floor(min + step * i));
  }
  return result;
};

/**
 * A hook for managing leverage in trading.
 *
 * @remarks
 * This hook provides functionality to get and update the user's leverage settings.
 *
 * It fetches the current leverage from client info and available leverage options from config.
 *
 * @returns A tuple containing:
 * - The current maximum leverage value
 * - An object with:
 *   - `update`: Function to update leverage
 *   - `isMutating`: Boolean indicating if an update is in progress
 *   - `config`: Array of available leverage options (e.g. [1, 2, 3, 4, 5, 10, 15, 20])
 *
 * @example
 * ```typescript
 * const [maxLeverage, { update, isMutating, config }] = useLeverage();
 *
 * // Get current max leverage
 * console.log(maxLeverage);
 *
 * // Update leverage
 * update({ leverage: 5 });
 *
 * // Available leverage options
 * console.log(config); // e.g., [1, 2, 3, 4, 5, 10, 15, 20]
 * ```
 */
export const useLeverage = () => {
  const { data, mutate } = usePrivateQuery<{ max_leverage: string | number }>(
    "/v1/client/info",
    {
      revalidateOnFocus: false,
    },
  );

  const [update, { isMutating }] = useMutation("/v1/client/leverage");

  const { data: leverageConfig, isLoading } = useQuery<{
    max_futures_leverage: string;
  }>("/v1/public/leverage", {
    revalidateOnFocus: false,
    errorRetryCount: 3,
    // formatter: (data) => data,
  });

  const updateLeverage = useCallback(
    async (data: { leverage: number }) => {
      const res = await update(data);
      if (res.success) {
        return mutate();
      } else {
        throw new Error(res.message);
      }
    },
    [update, mutate],
  );

  const memoizedCurLeverage = useMemo<number>(() => {
    if (data?.max_leverage !== undefined) {
      return Number(data.max_leverage);
    }
    return 1;
  }, [data?.max_leverage]);

  const memoizedMaxLeverage = useMemo<number>(() => {
    if (leverageConfig?.max_futures_leverage !== undefined) {
      return Number(leverageConfig.max_futures_leverage);
    }
    return 1;
  }, [leverageConfig?.max_futures_leverage]);

  const memoizedLeverageLevers = useMemo<number[]>(() => {
    return generateLeverageLevers(memoizedMaxLeverage);
  }, [memoizedMaxLeverage]);

  return {
    update: updateLeverage,
    isLoading: isLoading || isMutating,
    leverageLevers: memoizedLeverageLevers,
    curLeverage: memoizedCurLeverage,
    maxLeverage: memoizedMaxLeverage,
  } as const;
};
