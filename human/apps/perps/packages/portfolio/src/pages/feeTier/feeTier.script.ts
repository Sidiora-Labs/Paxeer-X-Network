/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useEffect, useMemo, useState } from "react";
import { on } from "events";
import {
  useAccount,
  useAccountInfo,
  usePrivateQuery,
} from "@paxeer-network/hooks";
import { useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum, API } from "@paxeer-network/types";
import type { Column, TanstackColumn } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useFeeTierColumns } from "./column";
import { dataSource as defaultDataSource } from "./dataSource";

export type useFeeTierScriptReturn = ReturnType<typeof useFeeTierScript>;

export type UseFeeTierScriptOptions = {
  dataAdapter?: (
    columns: Column<any>[],
    dataSource: any[],
    context?: {
      tier?: number;
    },
  ) => {
    columns: Column<any>[];
    dataSource: any[];
  };
  headerDataAdapter?: (original: any[]) => any[];
  onRow?: (
    record: any,
    index: number,
  ) => {
    normal?: any;
    active?: any;
  };
  onCell?: (
    column: TanstackColumn<any>,
    record: any,
    index: number,
  ) => {
    normal?: any;
    active?: any;
  };
};

export interface FeeDataType {
  tier: number | null;
  volume_min?: number | null;
  volume_max?: number | null;
  volume_node?: React.ReactNode;
  or?: string | null;
  maker_fee: string;
  taker_fee: string;
}

const findCurrentTier = (feeList: FeeDataType[], data: API.AccountInfo) => {
  const { futures_taker_fee_rate = 0, futures_maker_fee_rate = 0 } = data;
  const takerRate = new Decimal(futures_taker_fee_rate).mul(0.01);
  const makerRate = new Decimal(futures_maker_fee_rate).mul(0.01);
  const findItem = feeList.find((item) => {
    const itemTakerRate = item.taker_fee.split("/")?.[0].trim();
    const itemMakerRate = item.maker_fee.split("/")?.[0].trim();
    return (
      itemTakerRate === `${takerRate.toNumber()}%` &&
      itemMakerRate === `${makerRate.toNumber()}%`
    );
  });
  return findItem?.tier;
};

export const useFeeTierScript = (options?: UseFeeTierScriptOptions) => {
  const { dataAdapter, headerDataAdapter, onRow, onCell } = options || {};
  const [tier, setTier] = useState<number | null>();
  const { data, isLoading } = useAccountInfo();
  const { state } = useAccount();

  const cols = useFeeTierColumns();

  const { data: volumeStatistics } = usePrivateQuery<{
    perp_volume_last_30_days: number;
  }>("/v1/volume/user/stats");

  const { columns, dataSource } = useMemo(() => {
    return typeof dataAdapter === "function"
      ? dataAdapter(cols, defaultDataSource, { tier: tier! })
      : { columns: cols, dataSource: defaultDataSource };
  }, [dataAdapter, cols, tier]);

  useEffect(() => {
    if (!data || isLoading) {
      return;
    }
    setTier(findCurrentTier(dataSource, data));
  }, [data, isLoading, dataSource]);

  const authData = useDataTap(
    {
      tier: tier,
      vol: volumeStatistics?.perp_volume_last_30_days,
    },
    {
      accountStatus:
        state.status === AccountStatusEnum.EnableTradingWithoutConnected
          ? AccountStatusEnum.EnableTradingWithoutConnected
          : AccountStatusEnum.EnableTrading,
    },
  );

  return {
    ...authData,
    columns: columns,
    dataSource: dataSource,
    onRow: onRow,
    onCell: onCell,
    headerDataAdapter: headerDataAdapter,
  };
};
