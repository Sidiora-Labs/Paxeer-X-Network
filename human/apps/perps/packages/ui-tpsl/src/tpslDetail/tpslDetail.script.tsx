import { useCallback, useEffect, useMemo, useState } from "react";
import {
  findPositionTPSLFromOrders,
  useAccount,
  useEventEmitter,
  useOrderStream,
  useSubAccountAlgoOrderStream,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import {
  AlgoOrderRootType,
  API,
  OrderStatus,
  PositionType,
} from "@paxeer-network/types";
import { modal, useScreen } from "@paxeer-network/ui";
import { TPSLDialogId, TPSLSheetId } from "../positionTPSL/tpsl.widget";
import { TPSLDetailProps } from "./tpslDetail.widget";

type TPSLEditTarget = {
  algoOrderId: number;
  positionType: PositionType;
};

export const useTPSLDetail = (props: TPSLDetailProps) => {
  const { position } = props;

  const symbol = position.symbol;
  const marginMode = position.margin_mode;
  const symbolInfo = useSymbolsInfo()[symbol];

  const ee = useEventEmitter();

  const { state } = useAccount();

  const { isMobile } = useScreen();
  const [fullPositionOrders, setFullPositionOrders] = useState<API.AlgoOrder[]>(
    [],
  );
  const [partialPositionOrders, setPartialPositionOrders] = useState<
    API.AlgoOrder[]
  >([]);
  const [tpslEditTarget, setTPSLEditTarget] = useState<TPSLEditTarget>();
  const [tpslEditOpen, setTPSLEditOpen] = useState(false);

  const isSubAccount =
    position.account_id && position.account_id !== state.mainAccountId;

  const [
    mainAccountTpslOrders,
    { cancelAlgoOrder, cancelPostionOrdersByTypes },
  ] = useOrderStream(
    {
      symbol: position.symbol,
      status: OrderStatus.INCOMPLETE,
      includes: [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
      size: 500,
    },
    {
      keeplive: true,
    },
  );

  const [
    subAccountTpslOrders,
    {
      cancelAlgoOrder: cancelSubAccountAlgoOrder,
      cancelPostionOrdersByTypes: cancelSubAccountPostionOrdersByTypes,
      refresh: refreshSubAccountTpslOrders,
    },
  ] = useSubAccountAlgoOrderStream(
    {
      symbol: position.symbol,
      status: OrderStatus.INCOMPLETE,
      includes: [AlgoOrderRootType.POSITIONAL_TP_SL, AlgoOrderRootType.TP_SL],
      size: 100,
    },
    {
      accountId: position.account_id!,
    },
  );

  const tpslOrders = isSubAccount
    ? subAccountTpslOrders
    : mainAccountTpslOrders;

  const tpslEditOrder = useMemo(() => {
    if (!tpslEditTarget) {
      return undefined;
    }

    const orders =
      tpslEditTarget.positionType === PositionType.FULL
        ? fullPositionOrders
        : partialPositionOrders;

    return orders.find(
      (order) =>
        Number(order.algo_order_id) === Number(tpslEditTarget.algoOrderId),
    );
  }, [tpslEditTarget, fullPositionOrders, partialPositionOrders]);

  const onTPSLEditOpenChange = useCallback((open: boolean) => {
    setTPSLEditOpen(open);
    if (!open) {
      setTPSLEditTarget(undefined);
    }
  }, []);

  const onCancelOrder = async (order: API.AlgoOrder) => {
    if (isSubAccount) {
      const res = await cancelSubAccountAlgoOrder(
        order.algo_order_id,
        order.symbol,
      );
      refreshSubAccountTpslOrders();
      ee.emit("tpsl:updateOrder", position);
      return res;
    }
    return await cancelAlgoOrder(order.algo_order_id, order.symbol);
  };

  const onCancelAllTPSLOrders = async () => {
    if (isSubAccount) {
      const res = await cancelSubAccountPostionOrdersByTypes(symbol, [
        AlgoOrderRootType.TP_SL,
      ]);
      refreshSubAccountTpslOrders();
      ee.emit("tpsl:updateOrder", position);
      return res;
    }
    return await cancelPostionOrdersByTypes(symbol, [AlgoOrderRootType.TP_SL]);
  };

  const showTPSLDialog = (positionType: PositionType) => {
    const dialogId = isMobile ? TPSLSheetId : TPSLDialogId;
    modal.show(dialogId, {
      symbol: position.symbol,
      position,
      positionType,
      isEditing: false,
    });
  };

  const editTPSLOrder = (order: API.AlgoOrder, positionType: PositionType) => {
    setTPSLEditTarget({
      algoOrderId: order.algo_order_id,
      positionType,
    });
    setTPSLEditOpen(true);
  };

  const addTPSLOrder = (positionType: PositionType) => {
    showTPSLDialog(positionType);
  };

  useEffect(() => {
    if (tpslOrders) {
      const { fullPositionOrder, partialPositionOrders } =
        findPositionTPSLFromOrders(tpslOrders, symbol, marginMode);

      setFullPositionOrders(fullPositionOrder ? [fullPositionOrder] : []);
      setPartialPositionOrders(partialPositionOrders ?? []);
    }
  }, [tpslOrders, symbol, marginMode]);

  useEffect(() => {
    if (tpslEditOpen && tpslEditTarget && !tpslEditOrder) {
      onTPSLEditOpenChange(false);
    }
  }, [tpslEditOpen, tpslEditOrder, tpslEditTarget, onTPSLEditOpenChange]);

  return {
    symbolInfo,
    position,
    symbol,
    fullPositionOrders,
    partialPositionOrders,
    cancelPostionOrdersByTypes,
    onCancelOrder,
    onCancelAllTPSLOrders,
    editTPSLOrder,
    addTPSLOrder,
    tpslEditOrder,
    tpslEditOpen,
    tpslEditMode: isMobile ? ("sheet" as const) : ("dialog" as const),
    onTPSLEditOpenChange,
  };
};

export type TPSLDetailState = ReturnType<typeof useTPSLDetail>;
