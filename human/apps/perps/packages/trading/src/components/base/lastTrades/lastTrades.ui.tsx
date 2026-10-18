import React, { FC, ReactNode } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import { Box, cn, Grid, ListView, Text } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";
import { LastTradesState } from "./lastTrades.script";

export const LastTrades: FC<
  LastTradesState & {
    classNames?: {
      root?: string;
      list?: string;
      listHeader?: string;
      listItem?: {
        left?: string;
        mid?: string;
        right?: string;
      };
    };
    style?: React.CSSProperties;
  }
> = (props) => {
  return (
    <Box
      className={cn(
        "oui-lastTrades",
        "oui-grid oui-grid-rows=[auto,1fr] oui-h-full oui-w-full",
        props.classNames?.root,
      )}
      style={props.style}
    >
      <Box className="oui-pe-1">
        <Header
          base={props.base}
          quote={props.quote}
          className={props.classNames?.listHeader}
        />
      </Box>
      <List
        data={props.data}
        isLoading={props.isLoading}
        baseDp={props.baseDp}
        quoteDp={props.quoteDp}
        classNames={props.classNames?.listItem}
        className={props.classNames?.list}
      />
    </Box>
  );
};

const Row = (props: {
  key?: React.Key | null;
  classNames?: {
    root?: string;
    left?: string;
    mid?: string;
    right?: string;
  };
  left: ReactNode | string;
  mid: ReactNode | string;
  right: ReactNode | string;
}) => {
  const { left, mid, right, classNames } = props;
  return (
    // <Flex
    //   key={key}
    //   height={20}
    //   gap={2}
    //   width={"100%"}
    //   className={cn("oui-text-xs oui-tabular-nums", classNames?.root)}
    // >
    //   <Box className={cn("oui-flex-1", classNames?.left)}>{left}</Box>
    //   <Box className={cn("oui-flex-1", classNames?.mid)}>{mid}</Box>
    //   <Box className={cn("oui-flex-1 oui-text-end", classNames?.right)}>
    //     {right}
    //   </Box>
    // </Flex>

    <Grid
      cols={3}
      gapX={2}
      width="100%"
      className={cn("oui-text-xs oui-tabular-nums", classNames?.root)}
    >
      <div className={cn("oui-flex-1", classNames?.left)}>{left}</div>
      <div className={cn("oui-flex-1", classNames?.mid)}>{mid}</div>
      <div className={cn("oui-flex-1 oui-text-end", classNames?.right)}>
        {right}
      </div>
    </Grid>
  );
};

const Header = (props: { base: string; quote: string; className?: string }) => {
  const { t } = useTranslation();
  return (
    <Row
      left={t("common.time")}
      mid={`${t("common.price")}(${props.quote})`}
      right={`${t("common.qty")}(${props.base})`}
      classNames={{
        root: cn(
          "oui-text-base-contrast-54 oui-h-[32px] oui-sticky",
          props.className,
        ),
      }}
    />
  );
};

const List = (props: {
  data?: any[];
  isLoading?: boolean;
  baseDp: number;
  quoteDp: number;
  classNames?: {
    left?: string;
    mid?: string;
    right?: string;
  };
  className?: string;
}) => {
  return (
    <ListView
      dataSource={props.data}
      className={cn(
        "oui-last-trades-list",
        "oui-w-full oui-h-full",
        props.className,
        "oui-overflow-auto",
      )}
      contentClassName="!oui-space-y-0 -oui-me-[4px]"
      renderItem={(item, index) => {
        return (
          <Row
            key={index}
            left={
              <Text.formatted rule={"date"} formatString="HH:mm:ss">
                {item?.ts}
              </Text.formatted>
            }
            mid={commifyOptional(item?.price, { fix: props.quoteDp })}
            right={commifyOptional(item?.size, { fix: props.baseDp })}
            classNames={{
              left: cn("oui-text-base-contrast-80", props.classNames?.left),
              right: cn(
                item.side === OrderSide.BUY
                  ? "oui-text-trade-profit"
                  : "oui-text-trade-loss",
                props.classNames?.mid,
              ),
              mid: cn(
                item.side === OrderSide.BUY
                  ? "oui-text-trade-profit"
                  : "oui-text-trade-loss",
                props.classNames?.right,
              ),
            }}
          />
        );
      }}
    />
  );
};
