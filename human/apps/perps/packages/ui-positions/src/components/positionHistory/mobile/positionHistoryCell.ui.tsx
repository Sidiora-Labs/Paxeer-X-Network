import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Divider, Flex, Grid, modal, Text } from "@paxeer-network/ui";
import { SharePnLBottomSheetId } from "@paxeer-network/ui-share";
import { commifyOptional } from "@paxeer-network/utils";
import { ShareButtonWidget } from "../../positions/desktop/shareButton";
import {
  PositionHistoryType,
  ClosedQty,
  SymbolToken,
  OpenTime,
  AvgOpen,
  AvgClosed,
  ClosedTime,
  MaxClosedQty,
  FundingFee,
} from "./items";
import { PositionHistoryCellState } from "./positionHistoryCell.script";

export const PositionHistoryCell: FC<PositionHistoryCellState> = (props) => {
  return (
    <Flex
      direction={"column"}
      width={"100%"}
      gap={2}
      itemAlign={"start"}
      className={props.classNames?.root}
    >
      <Header {...props} />
      <Divider intensity={6} className="oui-w-full" />
      <Body {...props} />
    </Flex>
  );
};

export const Header: FC<PositionHistoryCellState> = (props) => {
  const { t } = useTranslation();

  const showAlert = () => {
    const { item: record } = props;

    modal.alert({
      title: t("positions.history.column.netPnl"),
      message: (
        <Flex
          direction={"column"}
          width={"100%"}
          gap={2}
          className="oui-text-2xs oui-text-base-contrast-54"
        >
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>{t("common.realizedPnl")}</Text>
            <Text color={record.realized_pnl >= 0 ? "profit" : "lose"}>
              {commifyOptional(record.realized_pnl)}
            </Text>
          </Flex>
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>{t("funding.fundingFee")}</Text>
            <Text
              color={record.accumulated_funding_fee > 0 ? "lose" : "profit"}
            >
              {commifyOptional(-record.accumulated_funding_fee)}
            </Text>
          </Flex>
          <Flex justify={"between"} width={"100%"} gap={2}>
            <Text intensity={54}>
              {t("positions.history.netPnl.tradingFee")}
            </Text>
            <Text color={record.trading_fee > 0 ? "lose" : "profit"}>
              {commifyOptional(-record.trading_fee)}
            </Text>
          </Flex>
        </Flex>
      ),
    });
  };

  return (
    <Flex gap={1} width={"100%"}>
      <Flex
        direction={"column"}
        itemAlign={"start"}
        className="oui-flex-1"
        gap={1}
      >
        <SymbolToken {...props} />
        <PositionHistoryType {...props} />
      </Flex>
      <Flex direction={"column"} itemAlign={"end"}>
        <Text size="2xs" intensity={36}>
          {t("positions.history.column.netPnl")}
        </Text>
        <Flex gapX={1}>
          <button onClick={showAlert}>
            <Text.numeral size="xs" coloring>
              {props.item.netPnL ?? "--"}
            </Text.numeral>
          </button>

          <ShareButtonWidget
            position={props.item}
            sharePnLConfig={props.sharePnLConfig}
            modalId={SharePnLBottomSheetId}
            iconSize={12}
            isPositionHistory
          />
        </Flex>
      </Flex>
    </Flex>
  );
};

export const Body: FC<PositionHistoryCellState> = (props) => {
  return (
    <div className="oui-w-full">
      <Grid
        cols={3}
        rows={2}
        width={"100%"}
        gap={0}
        className="oui-grid-cols-[1fr,1fr,135px]"
      >
        <ClosedQty {...props} />
        <MaxClosedQty {...props} />
        <OpenTime {...props} />
        <AvgOpen {...props} />
        <AvgClosed {...props} />
        <ClosedTime {...props} />
      </Grid>
      <FundingFee {...props} />
    </div>
  );
};
