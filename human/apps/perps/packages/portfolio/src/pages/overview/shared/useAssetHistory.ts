import { useCallback, useMemo, useRef, useState } from "react";
import { subDays, format } from "date-fns";
import {
  useAssetsHistory,
  useCollateral,
  useIndexPricesStream,
  useLocalStorage,
  usePrivateQuery,
  useStatisticsDaily,
  useUserStatistics,
  useBalanceTopic,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";

export enum PeriodType {
  WEEK = "7D",
  MONTH = "30D",
  QUARTER = "90D",
}

export const useAssetsHistoryData = (
  localKey: string,
  options?: { isRealtime?: boolean },
) => {
  const [today] = useState(() => new Date());

  const { getIndexPrice } = useIndexPricesStream();
  // const { account } = useAccount();

  const { t } = useTranslation();

  const { isRealtime = false } = options || {};
  const periodTypes = Object.values(PeriodType);

  const [period, setPeriod] = useLocalStorage<PeriodType>(
    localKey,
    PeriodType.WEEK,
  );

  const convertToUSDCAndOperate = useCallback(
    (inputs: {
      token: string;
      amount: string | number;
      value: Decimal;
      op?: "add" | "sub";
    }): Decimal => {
      const { token, amount, value, op = "sub" } = inputs;
      if (token.toUpperCase() === "USDC") {
        return op === "add" ? value.add(amount) : value.sub(amount);
      } else {
        const indexPrice = getIndexPrice(token);
        if (indexPrice) {
          const delta = new Decimal(amount).mul(indexPrice);
          return op === "add" ? value.add(delta) : value.sub(delta);
        }
        return value;
      }
    },
    [getIndexPrice],
  );

  const periodLabel = useMemo<Record<PeriodType, string>>(() => {
    return {
      [PeriodType.WEEK]: t("common.select.7d"),
      [PeriodType.MONTH]: t("common.select.30d"),
      [PeriodType.QUARTER]: t("common.select.90d"),
    };
  }, [t]);

  const { totalValue } = useCollateral();

  const getStartDate = (value: PeriodType) => {
    switch (value) {
      case PeriodType.MONTH:
        return subDays(today, 35);
      case PeriodType.QUARTER:
        return subDays(today, 95);
      default:
        return subDays(today, 10);
    }
  };

  const periodValue = useMemo(() => {
    switch (period) {
      case PeriodType.WEEK:
        return 7;
      case PeriodType.MONTH:
        return 30;
      case PeriodType.QUARTER:
        return 90;
      default:
        return 7;
    }
  }, [period]);

  const [startDate, setStartDate] = useState(getStartDate(period));
  // const nowStamp = useRef(new Date().getTime().toString());
  // const now = useRef(new Date());

  // Use 23:59:59 of the current day as the endDate to ensure coverage of the entire day's transactions.
  const endDate = useMemo(() => {
    const end = new Date(today);
    end.setHours(23, 59, 59, 999);
    return end;
  }, [today]);

  const totalDeposit = useRef<Decimal>(zero);

  // get transfer history
  const { data: transferOutHistory, mutate: mutateTransferOut } =
    usePrivateQuery<API.TransferHistory>(
      `/v1/internal_transfer_history?page=1&size=200&side=OUT&start_t=${startDate.getTime()}&end_t=${endDate.getTime()}`,
      {
        revalidateOnFocus: false,
      },
    );
  const { data: transferInHistory, mutate: mutateTransferIn } =
    usePrivateQuery<API.TransferHistory>(
      `/v1/internal_transfer_history?page=1&size=200&side=IN&start_t=${startDate.getTime()}&end_t=${endDate.getTime()}`,
      {
        revalidateOnFocus: false,
      },
    );

  const [data] = useStatisticsDaily(
    {
      startDate: startDate.toISOString().split("T")[0],
      endDate: endDate.toISOString().split("T")[0],
    },
    {
      ignoreAggregation: true,
    },
  );

  const [userStatistics] = useUserStatistics();

  const todayVolume = useMemo(
    () => userStatistics?.perp_trading_volume_today ?? 0,
    [userStatistics],
  );

  // Refresh the transfer history once after the balance change event.
  const lastBalanceEventRef = useRef<number>(0);
  const refreshTransferHistory = useCallback(() => {
    const now = Date.now();
    if (now - lastBalanceEventRef.current < 15000) {
      return;
    }
    lastBalanceEventRef.current = now;
    mutateTransferOut?.(undefined, true);
    mutateTransferIn?.(undefined, true);
  }, [mutateTransferOut, mutateTransferIn]);

  useBalanceTopic({
    onMessage: () => {
      refreshTransferHistory();
    },
  });

  // get deposit & withdraw records to calculate the current PNL
  const [assetHistory] = useAssetsHistory({
    startTime: subDays(today, 2).getTime(),
    endTime: endDate.getTime(),
    pageSize: 50,
  });

  // get all deposit records to calculate the ROI
  const [allDepositHistory] = useAssetsHistory({
    side: "DEPOSIT",
    startTime: subDays(today, periodValue).getTime(),
    endTime: endDate.getTime(),
    pageSize: 200,
  });

  const totalDepositForROI = useMemo(() => {
    return allDepositHistory
      ?.filter((item) => item.trans_status === "COMPLETED")
      .reduce((acc, item) => {
        return acc.add(
          // item.amount
          convertToUSDCAndOperate({
            token: item.token,
            amount: item.amount,
            value: zero,
            op: "add",
          }),
        );
      }, zero);
  }, [allDepositHistory, convertToUSDCAndOperate]);

  const totalTransferInForROI = useMemo(() => {
    if (!Array.isArray(transferInHistory)) {
      return zero;
    }
    return transferInHistory
      ?.filter((item) => item.status === "COMPLETED")
      .reduce((acc, item) => {
        return acc.add(
          convertToUSDCAndOperate({
            token: item.token,
            amount: item.amount,
            value: zero,
            op: "add",
          }),
        );
      }, zero);
  }, [transferInHistory, convertToUSDCAndOperate]);

  const onPeriodChange = (value: PeriodType) => {
    setStartDate(getStartDate(value));
    setPeriod(value);
  };

  // const lastItem = data[data.length - 1];

  const lastItem = useMemo(() => {
    return data.length > 0 ? data[data.length - 1] : null;
  }, [data]);

  const totalTransferIn = useMemo(
    () =>
      sumTransfersAfterSnapshot(
        transferInHistory,
        lastItem,
        convertToUSDCAndOperate,
      ),
    [transferInHistory, lastItem, convertToUSDCAndOperate],
  );

  const totalTransferOut = useMemo(
    () =>
      sumTransfersAfterSnapshot(
        transferOutHistory,
        lastItem,
        convertToUSDCAndOperate,
      ),
    [transferOutHistory, lastItem, convertToUSDCAndOperate],
  );

  const calculatedData = useMemo(() => {
    return calculateAssetHistoryData({
      data,
      realtime: isRealtime,
      totalValue,
      totalTransferIn,
      totalTransferOut,
      todayVol: todayVolume,
      periodValue,
      today,
      assetHistory,
      convertToUSDCAndOperate,
      totalDeposit,
    });
  }, [
    data,
    totalValue,
    assetHistory,
    isRealtime,
    getIndexPrice,
    totalTransferIn,
    totalTransferOut,
    todayVolume,
    periodValue,
  ]);

  const todayPerformance = useMemo(() => {
    if (!Array.isArray(calculatedData) || calculatedData.length === 0) {
      return null;
    }

    const todayRow = calculatedData[
      calculatedData.length - 1
    ] as API.DailyRow & {
      base_account_value?: number;
      __isCalculated?: boolean;
    };

    if (!todayRow.__isCalculated) {
      return null;
    }

    const baseAccountValue =
      typeof todayRow.base_account_value === "number"
        ? todayRow.base_account_value
        : (todayRow.account_value ?? 0);

    let roi = zero;
    if (baseAccountValue) {
      roi = new Decimal(todayRow.pnl).div(baseAccountValue);
    }

    return {
      pnl: todayRow.pnl,
      roi: roi.toNumber(),
      vol: todayRow.perp_volume ?? 0,
    };
  }, [calculatedData]);

  const aggregateValue = useMemo(() => {
    let vol = zero;
    let pnl = zero;
    let roi = zero;

    if (Array.isArray(calculatedData) && calculatedData.length) {
      calculatedData.forEach((d) => {
        vol = vol.add(d.perp_volume ?? 0);
        pnl = pnl.add(d.pnl);
      });

      const tail = calculatedData[0];

      const dataTailIndex = data.findIndex((d) => d.date === tail.date);

      const lastAccountValue = data[dataTailIndex - 1]?.account_value;

      if (typeof lastAccountValue === "undefined" || lastAccountValue === 0) {
        roi = zero;
      } else {
        roi = pnl.div(
          totalTransferInForROI.add(lastAccountValue).add(totalDepositForROI),
        );
      }
    }

    return { vol: vol.toNumber(), pnl: pnl.toNumber(), roi: roi.toNumber() };
  }, [calculatedData, data, totalTransferInForROI, totalDepositForROI]);

  const createFakeData = (
    start: Partial<API.DailyRow>,
    end: Partial<API.DailyRow>,
  ) => {
    return Array.from({ length: 2 }, (_, i) => {
      const date = format(i === 0 ? startDate : new Date(), "yyyy-MM-dd");
      return { date, ...(i === 0 ? start : end) };
    });
  };

  return {
    periodTypes,
    period: period as PeriodType,
    onPeriodChange: onPeriodChange,
    periodLabel: periodLabel,
    curPeriod: periodLabel[period as PeriodType],
    // data: calculatedDataUpdateByIndexPrice, // calculatedData,
    data: calculatedData, // calculatedData,
    aggregateValue,
    createFakeData,
    volumeUpdateDate: data?.[data.length - 1]?.date ?? "",
    todayPerformance,
  } as const;
};

export type useAssetsHistoryDataReturn = ReturnType<
  typeof useAssetsHistoryData
>;

function getUTCStr(date: Date) {
  const year = date.getUTCFullYear();
  const month = `0${date.getUTCMonth() + 1}`.slice(-2);
  const day = `0${date.getUTCDate()}`.slice(-2);

  return `${year}-${month}-${day}`;
}

type ConvertToUSDCAndOperate = (inputs: {
  token: string;
  amount: string | number;
  value: Decimal;
  op?: "add" | "sub";
}) => Decimal;

export function sumTransfersAfterSnapshot(
  transferHistory: unknown,
  lastItem: API.DailyRow | null,
  convertToUSDCAndOperate: ConvertToUSDCAndOperate,
): Decimal | null {
  if (!Array.isArray(transferHistory)) {
    return null;
  }
  const rows = transferHistory as API.TransferHistoryRow[];
  if (
    lastItem == null ||
    rows.length === 0 ||
    typeof lastItem.snapshot_time === "undefined"
  ) {
    return zero;
  }
  const snapshotTime = lastItem.snapshot_time;
  const list = rows.filter((item) => {
    return item.status === "COMPLETED" && item.created_time > snapshotTime;
  });
  return list.reduce((acc, item) => {
    return acc.add(
      convertToUSDCAndOperate({
        token: item.token,
        amount: item.amount,
        value: zero,
        op: "add",
      }),
    );
  }, zero);
}

export function calculateAssetHistoryData(inputs: {
  data: ReadonlyArray<API.DailyRow> | API.DailyRow[];
  realtime: boolean;
  totalValue: number | null;
  totalTransferIn: Decimal | null;
  totalTransferOut: Decimal | null;
  todayVol: number;
  periodValue: number;
  today: Date;
  assetHistory?: ReadonlyArray<API.AssetHistoryRow> | API.AssetHistoryRow[];
  convertToUSDCAndOperate: ConvertToUSDCAndOperate;
  totalDeposit: { current: Decimal };
}) {
  const {
    data,
    realtime,
    totalValue,
    totalTransferIn,
    totalTransferOut,
    todayVol,
    periodValue,
    today,
    assetHistory,
    convertToUSDCAndOperate,
    totalDeposit,
  } = inputs;

  /**
   * need the totalValue and data are all ready, else return null;
   */
  if (totalValue === null) {
    return [];
  }
  // if the transferOutHistory or transferInHistory is not ready, return null;

  if (totalTransferOut === null || totalTransferIn === null) {
    return [];
  }

  const calculateLastPnl = (inputs: {
    lastItem: API.DailyRow;
    assetHistory?: ReadonlyArray<API.AssetHistoryRow> | API.AssetHistoryRow[];
    transferHistory: {
      OUT: Decimal;
      IN: Decimal;
    };
    totalValue: number | null;
  }) => {
    if (totalValue == null) {
      return null;
    }
    // today daily pnl = totalValue - lastItem.account_value - deposit + withdraw
    let value = new Decimal(totalValue).sub(inputs.lastItem.account_value);

    // subtraction of deposit and withdraw
    if (
      Array.isArray(inputs.assetHistory) &&
      inputs.assetHistory.length > 0 &&
      typeof inputs.lastItem.snapshot_time !== "undefined"
    ) {
      // find a list with the timestamp greater than the last item timestamp and trans_status = success;
      const list = [];

      for (let i = 0; i < inputs.assetHistory.length; i++) {
        const item = inputs.assetHistory[i];
        if (item.created_time > inputs.lastItem.snapshot_time) {
          list.push(item);
        }
      }

      // calculate the sum of deposit and withdraw
      for (let i = 0; i < list.length; i++) {
        const item = list[i];
        if (item.side === "DEPOSIT") {
          if (item.trans_status === "COMPLETED") {
            value = convertToUSDCAndOperate({
              token: item.token,
              amount: item.amount,
              value,
              op: "sub",
            });

            totalDeposit.current = convertToUSDCAndOperate({
              token: item.token,
              amount: item.amount,
              value: totalDeposit.current,
              op: "add",
            });
          }
        } else if (item.side === "WITHDRAW") {
          if (item.trans_status !== "FAILED") {
            value = convertToUSDCAndOperate({
              token: item.token,
              amount: item.amount,
              value,
              op: "add",
            });
          }
        }
      }
    }

    value = value
      .sub(inputs.transferHistory.IN)
      .add(inputs.transferHistory.OUT);

    return value.toNumber();
  };

  const calculate = (
    data: ReadonlyArray<API.DailyRow> | API.DailyRow[],
    totalValue: number | null,
    totalTransferIn: Decimal,
    totalTransferOut: Decimal,
    todayVol: number,
  ) => {
    const lastItem = data[data.length - 1];

    return {
      ...lastItem,
      date: getUTCStr(today),
      perp_volume: todayVol,
      base_account_value: lastItem?.account_value ?? 0,
      account_value:
        totalValue !== null ? totalValue : (lastItem?.account_value ?? 0),
      pnl:
        calculateLastPnl({
          lastItem,
          assetHistory,
          totalValue,
          transferHistory: {
            OUT: totalTransferOut,
            IN: totalTransferIn,
          },
        }) ?? 0,
      __isCalculated: true,
    };
  };

  const mergeData = (
    data: ReadonlyArray<API.DailyRow> | API.DailyRow[],
    totalValue: number | null,
    totalTransferIn: Decimal,
    totalTransferOut: Decimal,
    todayVol: number,
  ) => {
    if (!Array.isArray(data) || data.length === 0) {
      return data;
    }

    if (data[data.length - 1].date === getUTCStr(today)) {
      return data;
    }

    return data.concat([
      calculate(data, totalValue, totalTransferIn, totalTransferOut, todayVol),
    ]);
  };

  const _data = !realtime
    ? data
    : mergeData(data, totalValue, totalTransferIn, totalTransferOut, todayVol);

  return _data.slice(Math.max(0, _data.length - periodValue));
}
