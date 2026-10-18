import React, { FC } from "react";
import { AlgoOrderRootType } from "@paxeer-network/types";
import { Divider, Flex, Grid, Text } from "@paxeer-network/ui";
import { TabType } from "../../orders.widget";
import { BracketOrderPriceWidget } from "./bracketOrderPrice";
import { CancelBtnWidget } from "./cancelBtn";
import { EditBtnWidget } from "./editBtn";
import {
  OrderTime,
  SymbolToken,
  OrderTypeView,
  Qty,
  Filled,
  EstTotal,
  TriggerPrice,
  LimitPrice,
  MarkPrice,
  TPTrigger,
  TPPrice,
  TPSLQuantity,
  SLTrigger,
  SLPrice,
  AvgPrice,
  OrderPrice,
  RealizedPnL,
  OrderState,
  Fee,
  TrailingCallback,
  TrailingPrice,
} from "./items";
import { OrderCellState } from "./orderCell.script";

export const OrderCell: FC<
  OrderCellState & {
    className?: string;
  }
> = (props) => {
  return (
    <Flex
      direction={"column"}
      width={"100%"}
      gap={2}
      itemAlign={"start"}
      className={props.className}
    >
      <Header {...props} />
      <Divider intensity={6} className="oui-w-full" />
      <Body {...props} />
      {props.type === TabType.pending && <BracketOrderPriceWidget {...props} />}
      {props.type !== TabType.orderHistory && <Btns {...props} />}
    </Flex>
  );
};

export const Header: FC<OrderCellState> = (props) => {
  return (
    <Flex direction={"column"} gap={1} width={"100%"}>
      <Flex justify={"between"} width={"100%"}>
        <SymbolToken {...props} />
        <OrderTime {...props} />
      </Flex>
      <Flex width={"100%"} justify={"between"}>
        <OrderTypeView {...props} />
        {props.type === TabType.orderHistory && <OrderState {...props} />}
      </Flex>
    </Flex>
  );
};

export const Body: FC<OrderCellState> = (props) => {
  return (
    <Grid cols={3} rows={2} width={"100%"} gap={1}>
      {itemsWithType(props)}
    </Grid>
  );
};

export const Btns: FC<OrderCellState> = (props) => {
  return (
    <Grid cols={3} rows={1} width={"100%"} gap={2}>
      <div></div>
      <EditBtnWidget state={props} />
      <CancelBtnWidget state={props} />
    </Grid>
  );
};

function itemsWithType(props: OrderCellState) {
  switch (props.type) {
    case TabType.all:
      return <></>;
    case TabType.pending:
      const isTrailingStop =
        props.item?.algo_type === AlgoOrderRootType.TRAILING_STOP;

      if (isTrailingStop) {
        return (
          <>
            <Qty {...props} />
            <Filled {...props} />
            <EstTotal {...props} />
            <TriggerPrice {...props} isPending={true} />
            <TrailingPrice {...props} />
            <TrailingCallback {...props} />
          </>
        );
      }

      return (
        <>
          <Qty {...props} />
          <Filled {...props} />
          <EstTotal {...props} />
          <TriggerPrice {...props} isPending={true} />
          <LimitPrice {...props} />
          <MarkPrice {...props} />
        </>
      );

    case TabType.tp_sl:
      return (
        <>
          <TPTrigger {...props} />
          <TPPrice {...props} />
          <TPSLQuantity {...props} />
          <SLTrigger {...props} />
          <SLPrice {...props} />
        </>
      );
    case TabType.filled:
      return <></>;
    case TabType.cancelled:
      return <></>;
    case TabType.rejected:
      return <></>;
    case TabType.orderHistory:
      return (
        <>
          <Qty {...props} />
          <Filled {...props} />
          <TriggerPrice {...props} align="end" />
          <AvgPrice {...props} />
          <OrderPrice {...props} />
          <RealizedPnL {...props} />
          {/* placeholder */}
          <div></div>
          {/* placeholder */}
          <div></div>
          <Fee {...props} />
        </>
      );
  }
}
