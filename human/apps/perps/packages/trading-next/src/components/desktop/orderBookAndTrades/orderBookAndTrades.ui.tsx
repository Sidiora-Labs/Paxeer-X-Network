import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, Flex, Grid, TabPanel, Tabs, Text } from "@paxeer-network/ui";
import type { OrderBookAndTradesState } from "./orderBookAndTrades.script";

const LazyLastTradesWidget = React.lazy(() =>
  import("../../base/lastTrades").then((mod) => {
    return {
      default: mod.LastTradesWidget,
    };
  }),
);

const LazyOrderBookWidget = React.lazy(() =>
  import("../../base/orderBook").then((mod) => {
    return {
      default: mod.OrderBookWidget,
    };
  }),
);

const TwoColLayout: React.FC<OrderBookAndTradesState> = (props) => {
  const { t } = useTranslation();
  return (
    <Grid
      cols={2}
      width={"100%"}
      gap={3}
      className="oui-auto-rows-fr"
      style={{
        height: props.containerSize?.height,
      }}
    >
      <Flex
        direction={"column"}
        itemAlign={"start"}
        // pl={3}
        pt={3}
        pb={3}
        r="2xl"
        className="oui-bg-base-9"
      >
        <Title
          title={t("trading.orderBook")}
          className="oui-ps-3 oui-text-sm"
        />
        <React.Suspense fallback={null}>
          <LazyOrderBookWidget
            symbol={props.symbol}
            height={
              props.containerSize
                ? props.containerSize.height - 29 - 24
                : undefined
            }
          />
        </React.Suspense>
      </Flex>
      <Flex
        direction={"column"}
        itemAlign={"start"}
        py={3}
        r="2xl"
        className="oui-bg-base-9 oui-h-full"
      >
        <Title
          title={t("trading.lastTrades")}
          className="oui-text-sm oui-px-3"
        />
        <React.Suspense fallback={null}>
          <LazyLastTradesWidget
            symbol={props.symbol}
            style={{
              height:
                props.containerSize && props.containerSize.height - 29 - 24,
            }}
            classNames={{ listHeader: "oui-px-3", list: "oui-px-3" }}
          />
        </React.Suspense>
      </Flex>
    </Grid>
  );
};

const TabLayout: React.FC<OrderBookAndTradesState> = (props) => {
  const { t } = useTranslation();

  return (
    <Box
      // pl={3}
      pt={3}
      pb={3}
      pr={0}
      r="2xl"
      className="oui-bg-base-9"
      style={{ maxHeight: props.containerSize?.height }}
    >
      <Tabs
        value={props.tab}
        variant="contained"
        onValueChange={(tab) => {
          props.setTab(tab as any);
        }}
        classNames={{
          tabsList: "oui-ps-3",
          // tabsContent: props.tab === "lastTrades" ? 'oui-ps-3' : ''
        }}
        size="lg"
      >
        <TabPanel value="orderBook" title={t("trading.orderBook")}>
          <React.Suspense fallback={null}>
            <LazyOrderBookWidget
              symbol={props.symbol}
              height={
                props.containerSize
                  ? props.containerSize.height - 29 - 18
                  : undefined
              }
            />
          </React.Suspense>
        </TabPanel>
        <TabPanel value="lastTrades" title={t("trading.lastTrades")}>
          <React.Suspense fallback={null}>
            <LazyLastTradesWidget
              symbol={props.symbol}
              style={{
                height:
                  props.containerSize && props.containerSize.height - 29 - 18,
              }}
              classNames={{
                root: "oui-pt-[6px]",
                listHeader: "oui-px-3",
                list: "oui-px-3",
              }}
            />
          </React.Suspense>
        </TabPanel>
      </Tabs>
    </Box>
  );
};

const Title: React.FC<{ title: string; className?: string }> = (props) => {
  return (
    <Text
      size="base"
      intensity={80}
      className={cn("oui-pb-[5px]", props.className)}
    >
      {props.title}
    </Text>
  );
};

export const OrderBookAndTrades: React.FC<OrderBookAndTradesState> = (
  props,
) => {
  const { containerRef, containerSize } = props;
  return (
    <div
      ref={containerRef}
      className="oui-trading-orderBookAndTrades oui-h-full"
    >
      {(containerSize?.width ?? 0) >= 572 ? (
        <TwoColLayout {...props} />
      ) : (
        <TabLayout {...props} />
      )}
    </div>
  );
};
