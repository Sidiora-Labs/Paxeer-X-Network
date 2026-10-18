import React, { useEffect, useMemo } from "react";
import {
  useBadgeBySymbol,
  useGetRwaSymbolInfo,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  MarketsSheetWidget,
  SymbolInfoBarRiskNotice,
  SymbolInfoBarWidget,
} from "@paxeer-network/markets";
import {
  Box,
  SimpleSheet,
  EyeCloseIcon,
  EyeIcon,
  Flex,
  Text,
  NewsFillIcon,
} from "@paxeer-network/ui";
import { Countdown } from "../../components/base/countdown";
import { showRwaOutsideMarketHoursNotify } from "../../components/desktop/notify/rwaNotification";
import type { TradingState } from "./trading.script";

const LazyTopTabWidget = React.lazy(() =>
  import("../../components/mobile/topTab").then((mod) => {
    return { default: mod.TopTabWidget };
  }),
);

const LazyOrderBookAndEntryWidget = React.lazy(() =>
  import("../../components/mobile/orderBookAndEntry").then((mod) => {
    return { default: mod.OrderBookAndEntryWidget };
  }),
);

const LazyDataListWidget = React.lazy(() =>
  import("../../components/mobile/dataList").then((mod) => {
    return { default: mod.DataListWidget };
  }),
);

const MaybeEqual: React.FC = () => {
  return (
    <svg
      width="8"
      height="8"
      viewBox="0 0 8 8"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast-54"
    >
      <path d="M6.456 3.48a3.5 3.5 0 0 0 .431-.567 9 9 0 0 0 .361-.654l-.648-.66-.107.185q-.071.12-.142.244a3 3 0 0 1-.27.396 1.4 1.4 0 0 1-.318.29.67.67 0 0 1-.38.114q-.307 0-.666-.194t-.742-.42q-.383-.227-.777-.42a1.7 1.7 0 0 0-.771-.194q-.401.001-.72.154a2.1 2.1 0 0 0-.57.404 2.6 2.6 0 0 0-.43.574 5 5 0 0 0-.307.646l.649.66q.165-.437.464-.833.298-.395.742-.396.347 0 .7.194.354.193.721.42.37.227.763.42.396.195.826.195.374.001.665-.155.29-.151.526-.404m.352 2.941a3.5 3.5 0 0 0 .431-.566q.196-.315.361-.654l-.648-.66-.107.184-.142.244a3 3 0 0 1-.27.396 1.4 1.4 0 0 1-.318.29.67.67 0 0 1-.38.115q-.307 0-.666-.195-.36-.193-.742-.42-.383-.226-.778-.42a1.7 1.7 0 0 0-.77-.194q-.401.001-.72.154a2.1 2.1 0 0 0-.57.405 2.6 2.6 0 0 0-.43.574 5 5 0 0 0-.307.646l.649.66q.165-.437.464-.833.297-.395.742-.396.347 0 .7.194.354.193.721.42.369.226.763.42.396.194.826.194.374.001.665-.154.29-.152.526-.404" />
    </svg>
  );
};

export const MobileLayout: React.FC<TradingState> = (props) => {
  const { t } = useTranslation();

  const { isRwa, open, closeTimeInterval } = useGetRwaSymbolInfo(props.symbol);
  const symbolsInfo = useSymbolsInfo();
  const isPreTge = Boolean(symbolsInfo[props.symbol]?.("is_pretge"));
  const { brokerId, brokerName, brokerNameRaw, displaySymbolName } =
    useBadgeBySymbol(props.symbol);
  const isCommunityListed = Boolean(brokerId ?? brokerName);
  const baseFromSymbol = props.symbol?.split("_")[1] ?? props.symbol ?? "";
  const symbolWithBroker =
    brokerName != null
      ? `${baseFromSymbol}-${brokerNameRaw}`
      : (displaySymbolName ?? props.symbol ?? "");

  useEffect(() => {
    if (isRwa && !open) {
      showRwaOutsideMarketHoursNotify();
    }
  }, [isRwa, open, props.symbol]);

  const rwaStatusBar = useMemo(() => {
    if (!isRwa) {
      return null;
    }

    if (closeTimeInterval === undefined) {
      return null;
    }

    const thresholdTime = 30 * 60;

    if ((closeTimeInterval ?? 0) > thresholdTime) {
      return null;
    }

    return (
      <Flex
        gap={1}
        p={2}
        justify="start"
        itemAlign="center"
        r="lg"
        mt={2}
        className="oui-bg-success/15 oui-text-xs oui-text-base-contrast-54"
      >
        <NewsFillIcon color="success" size={16} className="oui-flex-shrink-0" />
        <Flex className="oui-flex-1 oui-text-success">
          <Text>
            {open
              ? t("trading.rwa.mWeb.outsideMarketHours.desc")
              : t("trading.rwa.mWeb.insideMarketHours.desc")}
          </Text>
        </Flex>
        <Countdown timeInterval={closeTimeInterval} />
      </Flex>
    );
  }, [isRwa, open, closeTimeInterval, t]);

  const symbolInfoBar = (
    <SymbolInfoBarWidget
      symbol={props.symbol}
      onSymbol={() => props.onOpenMarketsSheetChange(true)}
      trailing={
        <Flex
          direction={"column"}
          itemAlign={"end"}
          className="oui-cursor-pointer oui-text-[11px]"
          onClick={props.onShowPortfolioSheet}
        >
          <Flex>
            <Text intensity={54}>{t("common.totalValue")}</Text>
            <button
              onClick={(e) => {
                e.stopPropagation();
                e.preventDefault();
                props.setHideAssets(!props.hideAssets);
              }}
              className="oui-px-1"
            >
              {props.hideAssets ? (
                <EyeIcon
                  color="primary"
                  opacity={1}
                  size={16}
                  className="oui-text-primary-light"
                />
              ) : (
                <EyeCloseIcon
                  color="primary"
                  opacity={1}
                  size={16}
                  className="oui-text-primary-light"
                />
              )}
            </button>
            <MaybeEqual />
          </Flex>
          <Text.numeral
            suffix={<Text intensity={20}>&nbsp;USDC</Text>}
            dp={2}
            visible={!props.hideAssets}
          >
            {props.canTrade ? (props.total ?? "--") : "--"}
          </Text.numeral>
        </Flex>
      }
    />
  );

  /** Community-listed symbols: show broker risk strip above the symbol bar. */
  const topBar = (
    <Box>
      <Flex mx={1}>
        <SymbolInfoBarRiskNotice
          visible={isCommunityListed}
          symbolWithBroker={symbolWithBroker}
          brokerName={brokerNameRaw ?? brokerName ?? ""}
          isPreTge={isPreTge}
          autoHeight
          className="oui-my-1"
        />
      </Flex>
      {symbolInfoBar}
      <SimpleSheet
        open={props.openMarketsSheet}
        onOpenChange={props.onOpenMarketsSheetChange}
        classNames={{
          body: "oui-h-full oui-pb-0",
          content: "!oui-w-full !oui-max-w-none !oui-p-0",
        }}
        contentProps={{
          side: "left",
          closeClassName:
            "!oui-end-1 oui-flex oui-size-11 oui-items-center oui-justify-center",
        }}
      >
        <MarketsSheetWidget
          symbol={props.symbol}
          onSymbolChange={(symbol) => {
            props.onOpenMarketsSheetChange(false);
            props.onSymbolChange?.(symbol);
          }}
        />
      </SimpleSheet>
      {rwaStatusBar}
    </Box>
  );

  return (
    <div className="oui-relative oui-grid oui-gap-1 oui-bg-base-10">
      <main className="oui-hide-scrollbar oui-space-y-1 oui-overflow-y-auto">
        {topBar}

        <React.Suspense fallback={null}>
          <LazyTopTabWidget className="oui-mx-1 oui-rounded-xl oui-bg-base-9" />
        </React.Suspense>
        <React.Suspense fallback={null}>
          <LazyOrderBookAndEntryWidget />
        </React.Suspense>
        <React.Suspense fallback={null}>
          <LazyDataListWidget
            symbol={props.symbol}
            className="oui-mx-1 oui-rounded-xl"
            sharePnLConfig={props.sharePnLConfig}
          />
        </React.Suspense>
      </main>
    </div>
  );
};
