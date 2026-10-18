import { useCallback, useEffect, useMemo, useState } from "react";
import {
  useAccount,
  useAssetsHistory,
  useBalanceTopic,
  useDebouncedCallback,
  useLocalStorage,
  useTransferHistory,
  useWalletTopic,
} from "@paxeer-network/hooks";
import {
  AccountStatusEnum,
  API,
  AssetHistorySideEnum,
  AssetHistoryStatusEnum,
} from "@paxeer-network/types";
import { useTransactionTime } from "../../contract/useTransactionTime";

export type DepositStatusScriptReturn = ReturnType<
  typeof useDepositStatusScript
>;

function getTimeRange() {
  const endTime = Date.now();
  // 1 hour ago
  const startTime = endTime - 1000 * 60 * 60;

  return [startTime, endTime];
}

export function useDepositStatusScript() {
  const [depositDataRange, setDepositDataRange] = useState(getTimeRange());
  const [transferDataRange, setTransferDataRange] = useState(getTimeRange());
  // only deposit have pending status
  const [depositPending, setDepositPending] = useState(0);
  const [depositCompleted, setDepositCompleted] = useState(0);
  const [transferCompleted, setTransferCompleted] = useState(0);
  const [pendingList, setPendingList] = useState<API.TransferHistoryRow[]>([]);

  const [hideNotificationTimeMap, setHideNotificationTimeMap] = useLocalStorage(
    "paxeerNetwork_deposit_status_hide_notification_time",
    {
      [AssetHistoryStatusEnum.PENDING]: 0,
      [AssetHistoryStatusEnum.COMPLETED]: 0,
    },
  );

  const { state } = useAccount();

  const canTrade =
    state.status === AccountStatusEnum.EnableTrading ||
    state.status === AccountStatusEnum.EnableTradingWithoutConnected;

  const { chainId, blockTime } = useMemo(() => {
    if (canTrade && pendingList.length === 1) {
      return {
        chainId: pendingList[0].chain_id,
        blockTime: pendingList[0].block_time,
      };
    }

    return {
      chainId: undefined,
      blockTime: undefined,
    };
  }, [canTrade, pendingList]);

  const transactionTime = useTransactionTime(chainId);

  // Estimated time remaining = tx_block_time +(total confirmations x average block time) - current time
  const estimatedTime = useMemo(() => {
    if (transactionTime && blockTime) {
      const seconds = blockTime / 1000 + transactionTime - Date.now() / 1000;
      console.log("transactionTime", transactionTime, seconds);
      return formatEstimatedTime(seconds);
    }

    return 0;
  }, [transactionTime, blockTime]);

  const [assetHistory, { isLoading }] = useAssetsHistory(
    {
      startTime: depositDataRange[0],
      endTime: depositDataRange[1],
      page: 1,
      pageSize: 200,
      side: "DEPOSIT",
    },
    {
      shouldUpdateOnWalletChanged: () => false,
    },
  );

  // pending and completed use one request to reduce api request， because assets history api limit 10 per 60s
  const [transferHistory] = useTransferHistory({
    dataRange: transferDataRange,
    side: "IN",
    size: 200,
    page: 1,
    main_sub_only: false,
  });

  // update time range when wallet and balance changed
  // because DEPOSIT and WITHDRAW will push twice COMPLETED event in a shorttime, so we need to debounce it
  // TODO: refresh one when push more than twice again
  const updateDepositTimeRange = useDebouncedCallback((data: any) => {
    setDepositDataRange(getTimeRange());
  }, 300);

  useWalletTopic({
    onMessage(data) {
      const { side, transStatus } = data;
      if (
        side === AssetHistorySideEnum.DEPOSIT &&
        (transStatus === AssetHistoryStatusEnum.PENDING ||
          transStatus === AssetHistoryStatusEnum.COMPLETED)
      ) {
        console.log("deposit status updated", data);
        updateDepositTimeRange(data);
      }
    },
  });

  // update transfer time range when balance changed
  const updateTransferTimeRange = useDebouncedCallback((data: any) => {
    setTransferDataRange(getTimeRange());
  }, 300);

  useBalanceTopic({
    onMessage(data) {
      console.log("balance updated", data);
      updateTransferTimeRange(data);
    },
  });

  useEffect(() => {
    if (!assetHistory || isLoading) {
      return;
    }
    const hidePendingNotificationTime =
      hideNotificationTimeMap[AssetHistoryStatusEnum.PENDING];

    const hideCompletedNotificationTime =
      hideNotificationTimeMap[AssetHistoryStatusEnum.COMPLETED];

    let pendingList = assetHistory.filter(
      (item) => item.trans_status === AssetHistoryStatusEnum.PENDING,
    );

    if (hidePendingNotificationTime) {
      pendingList = pendingList.filter(
        (item) => item.created_time > hidePendingNotificationTime,
      );
    }

    let completedList = assetHistory.filter(
      (item) => item.trans_status === AssetHistoryStatusEnum.COMPLETED,
    );

    if (hideCompletedNotificationTime) {
      completedList = completedList.filter(
        (item) => item.created_time > hideCompletedNotificationTime,
      );
    }

    setDepositPending(pendingList?.length || 0);
    setDepositCompleted(completedList?.length || 0);
    setPendingList(pendingList || []);
  }, [assetHistory, isLoading, hideNotificationTimeMap]);

  useEffect(() => {
    if (!transferHistory) {
      return;
    }
    const hideCompletedNotificationTime =
      hideNotificationTimeMap[AssetHistoryStatusEnum.COMPLETED];

    const completedList = hideCompletedNotificationTime
      ? transferHistory.filter(
          (item) => item.created_time > hideCompletedNotificationTime,
        )
      : transferHistory;

    setTransferCompleted(completedList.length);
  }, [transferHistory, hideNotificationTimeMap]);

  const completedCount = useMemo(() => {
    return depositCompleted + transferCompleted;
  }, [depositCompleted, transferCompleted]);

  const onClose = useCallback(
    (status: AssetHistoryStatusEnum) => {
      setHideNotificationTimeMap({
        ...hideNotificationTimeMap,
        [status]: Date.now(),
      });
    },
    [hideNotificationTimeMap],
  );

  return {
    pendingCount: depositPending,
    completedCount,
    canTrade,
    estimatedTime,
    onClose,
  };
}

const MIN_ESTIMATED_SECONDS = 30;
const MAX_ESTIMATED_SECONDS = 30 * 60; // 30 minutes

function formatEstimatedTime(totalSeconds: number) {
  const sec = Math.min(
    MAX_ESTIMATED_SECONDS,
    Math.max(MIN_ESTIMATED_SECONDS, totalSeconds),
  );
  let minutes = Math.floor(sec / 60);
  let seconds = sec % 60;

  if (seconds > 0 && seconds <= 30) {
    seconds = 30;
  } else if (seconds > 30) {
    minutes += 1;
    seconds = 0;
  }

  if (minutes > 0) {
    return seconds > 0 ? `${minutes} m ${seconds} s` : `${minutes} m`;
  }

  return `${seconds} s`;

  // minutes = Math.max(1, Math.ceil(seconds / 60));
}
