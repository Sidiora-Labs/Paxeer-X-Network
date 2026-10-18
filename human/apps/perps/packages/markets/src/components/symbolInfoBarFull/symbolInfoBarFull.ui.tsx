import React, { ReactNode } from "react";
import {
  useBadgeBySymbol,
  useFundingRate,
  useSymbolsInfo,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  TokenIcon,
  Flex,
  Text,
  cn,
  Divider,
  NewsFillIcon,
  Box,
  Tooltip,
  WarningIcon,
} from "@paxeer-network/ui";
import { CloseIcon } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import {
  ArrowLeftIcon,
  FavoritesIcon2,
  TriangleDownIcon,
  UnFavoritesIcon2,
} from "../../icons";
import { FundingRateHintWidget } from "../fundingRateHint";
import type { MarketsProviderProps } from "../marketsProvider";
import { SymbolBadge } from "../symbolBadge";
import { SymbolDisplay } from "../symbolDisplay";
import { RwaTooltip } from "./rwaTooltip";
import type { UseSymbolInfoBarFullScriptReturn } from "./symbolInfoBarFull.script";

const LazyDropDownMarketsWidget = React.lazy(() =>
  import("../dropDownMarkets").then((mod) => {
    return { default: mod.DropDownMarketsWidget };
  }),
);

const LazyFavoritesDropdownMenuWidget = React.lazy(() =>
  import("../favoritesDropdownMenu").then((mod) => {
    return { default: mod.FavoritesDropdownMenuWidget };
  }),
);

const LazyDataItem = React.lazy(() =>
  import("./dataItem.ui").then((mod) => {
    return { default: mod.DataItem };
  }),
);

const RISK_NOTICE_LEARN_MORE_URL =
  "https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/permissionless-listing";

/** Part 1: Community-listed risk notice banner */
export const SymbolInfoBarRiskNotice: React.FC<{
  className?: string;
  visible: boolean;
  symbolWithBroker: string;
  brokerName: string;
  isPreTge?: boolean;
  /** When true, height follows content instead of fixed 46px (e.g. for mobile) */
  autoHeight?: boolean;
}> = ({
  className,
  visible,
  symbolWithBroker,
  brokerName,
  isPreTge,
  autoHeight,
}) => {
  const { t } = useTranslation();
  if (!visible) return null;
  const contentKey = isPreTge
    ? "markets.symbolInfoBar.riskNotice.preLaunch.content"
    : "markets.symbolInfoBar.riskNotice.content";
  return (
    <a
      href={RISK_NOTICE_LEARN_MORE_URL}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(
        "oui-symbol-info-bar-risk-notice oui-w-full oui-bg-warning-darken/10 oui-rounded-none oui-text-warning-darken hover:oui-cursor-pointer oui-block",
        className,
      )}
    >
      <Flex
        itemAlign="center"
        gap={2}
        className={
          autoHeight ? "oui-min-h-0 oui-py-2 oui-px-2" : "oui-h-[46px] oui-px-3"
        }
      >
        <WarningIcon className="oui-shrink-0 oui-size-4 oui-text-warning-darken" />
        <Text size="xs" className="oui-text-warning-darken oui-flex-1">
          {t(contentKey, {
            symbolWithBroker,
            brokerName,
          })}{" "}
          <span className="oui-underline">{t("common.learnMore")}</span>
        </Text>
      </Flex>
    </a>
  );
};

/** Part 2: Desktop info bar (favorites, symbol, last price, scrollable data area) */
type SymbolInfoBarDesktopProps = {
  className?: string;
  trailing?: ReactNode;
  leftSection: ReactNode;
  priceSection: ReactNode;
  scrollableContent: ReactNode;
  containerRef: React.LegacyRef<HTMLDivElement>;
  leadingVisible: boolean;
  tailingVisible: boolean;
  onScroll: (direction: string) => void;
};

const SymbolInfoBarDesktop: React.FC<SymbolInfoBarDesktopProps> = ({
  className,
  trailing,
  leftSection,
  priceSection,
  scrollableContent,
  containerRef,
  leadingVisible,
  tailingVisible,
  onScroll,
}) => (
  <Flex
    intensity={900}
    r="2xl"
    // mt={1}
    px={3}
    className={cn(
      "oui-symbol-info-bar-desktop",
      "oui-font-semibold",
      "oui-flex-1 oui-w-full",
      "oui-py-2",
      className,
    )}
    style={{
      transform: "translateZ(0)",
      willChange: "transform",
    }}
  >
    <Flex gapX={6} className="oui-h-full oui-flex-1 oui-overflow-hidden">
      <Flex gapX={1} className="oui-flex-none oui-shrink-0">
        {leftSection}
      </Flex>
      <Divider className="oui-h-[26px]" direction="vertical" intensity={8} />
      {priceSection}
      <div className="oui-relative oui-h-full oui-overflow-hidden">
        <div
          ref={containerRef}
          className="oui-hide-scrollbar oui-h-full oui-overflow-x-auto"
        >
          {scrollableContent}
        </div>
        <ScrollIndicator leading onClick={onScroll} visible={leadingVisible} />
        <ScrollIndicator tailing onClick={onScroll} visible={tailingVisible} />
      </div>
    </Flex>
    {trailing}
  </Flex>
);

export type Layout = "left" | "right";

export type SymbolInfoBarFullProps = Pick<
  MarketsProviderProps,
  "onSymbolChange"
> &
  UseSymbolInfoBarFullScriptReturn & {
    className?: string;
    trailing?: ReactNode;
    closeCountdown?: () => void;
    showCountdown?: boolean;
  };

export const SymbolInfoBarFull: React.FC<SymbolInfoBarFullProps> = (props) => {
  const {
    symbol,
    isFavorite,
    favorite,
    data,
    quotoDp,
    openInterest,
    containerRef,
    leadingElementRef,
    tailingElementRef,
    leadingVisible,
    tailingVisible,
    onScoll,
    fundingPeriod,
    capFunding,
    floorFunding,
    isRwa,
    open,
    closeTimeInterval,
    openTimeInterval,
    closeCountdown,
    showCountdown,
  } = props;

  const { t } = useTranslation();
  const symbolsInfo = useSymbolsInfo();
  const { brokerId, brokerName, brokerNameRaw, displaySymbolName } =
    useBadgeBySymbol(symbol);
  const isPreTge = Boolean(symbolsInfo[symbol]?.("is_pretge"));
  const isCommunityListed = Boolean(brokerId ?? brokerName);
  const baseFromSymbol = symbol?.split("_")[1] ?? symbol;
  const symbolWithBroker =
    brokerName != null
      ? `${baseFromSymbol}-${brokerNameRaw}`
      : (displaySymbolName ?? symbol);

  const favoriteIcon = (
    <React.Suspense fallback={null}>
      <LazyFavoritesDropdownMenuWidget row={{ symbol }} favorite={favorite}>
        <Flex
          width={12}
          height={12}
          justify="center"
          itemAlign="center"
          className="oui-me-1 oui-cursor-pointer"
        >
          {isFavorite ? (
            <FavoritesIcon2 className="oui-size-3 oui-text-warning-darken" />
          ) : (
            <UnFavoritesIcon2 className="oui-size-3 oui-text-base-contrast-36 hover:oui-text-warning-darken" />
          )}
        </Flex>
      </LazyFavoritesDropdownMenuWidget>
    </React.Suspense>
  );

  const symbolView = (
    <React.Suspense fallback={null}>
      <Flex
        direction="column"
        itemAlign="start"
        className="oui-shrink-0 oui-gap-y-[2px]"
      >
        <LazyDropDownMarketsWidget
          contentClassName="oui-w-[620px] oui-h-[496px]"
          symbol={props.symbol}
          onSymbolChange={props.onSymbolChange}
        >
          <Flex direction="column" gap={1}>
            <Flex gapX={1} className="oui-cursor-pointer">
              <TokenIcon symbol={symbol} className="oui-size-4" />
              <SymbolDisplay
                formatString="base"
                size="xs"
                className="oui-text-base-contrast"
                showBadge={false}
              >
                {symbol}
              </SymbolDisplay>
              <TriangleDownIcon className="oui-text-base-contrast-54" />
            </Flex>
          </Flex>
        </LazyDropDownMarketsWidget>
        <Flex gap={1}>
          <SymbolBadge symbol={symbol} />
          {isRwa && (
            <RwaTooltip
              isRwa={isRwa}
              open={open}
              closeTimeInterval={closeTimeInterval}
              openTimeInterval={openTimeInterval}
            />
          )}
        </Flex>
      </Flex>
    </React.Suspense>
  );

  const price = (
    <Text.numeral
      dp={quotoDp || 2}
      currency="$"
      size="sm"
      intensity={98}
      className="oui-data-value"
    >
      {data?.["24h_close"]}
    </Text.numeral>
  );

  const change = (
    <>
      <Text.numeral coloring rm={Decimal.ROUND_DOWN} showIdentifier>
        {data?.["24h_change"]!}
      </Text.numeral>
      <Text intensity={36}>/</Text>
      <Text.numeral
        rule="percentages"
        coloring
        rm={Decimal.ROUND_DOWN}
        showIdentifier
      >
        {data?.["change"]!}
      </Text.numeral>
    </>
  );

  const leftSection = (
    <>
      {favoriteIcon}
      {symbolView}
    </>
  );

  const priceSection = (
    <Tooltip
      content={t("markets.symbolInfoBar.lastPrice.tooltip")}
      className="oui-max-w-[240px]"
    >
      <span className="oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12 oui-inline-block">
        {price}
      </span>
    </Tooltip>
  );

  const scrollableContent = (
    <Flex gapX={8} height="100%">
      <div ref={leadingElementRef}>
        <React.Suspense fallback={null}>
          <LazyDataItem label={t("markets.column.24hChange")} value={change} />
        </React.Suspense>
      </div>
      <React.Suspense fallback={null}>
        <LazyDataItem
          label={t("markets.symbolInfoBar.Mark")}
          value={
            <Text.numeral
              dp={quotoDp}
              data-testid="oui-testid-tokenInfo-markPrice-value"
            >
              {data?.["mark_price"]}
            </Text.numeral>
          }
          hint={t("markets.symbolInfoBar.Mark.tooltip")}
        />
      </React.Suspense>
      <React.Suspense fallback={null}>
        <LazyDataItem
          label={t("markets.symbolInfoBar.Index")}
          value={
            <Text.numeral dp={quotoDp}>{data?.["index_price"]}</Text.numeral>
          }
          hint={t("markets.symbolInfoBar.Index.tooltip")}
        />
      </React.Suspense>
      <React.Suspense fallback={null}>
        <LazyDataItem
          label={t("markets.symbolInfoBar.24hVolume")}
          value={
            <Text.numeral rule="human" dp={2}>
              {data?.["24h_amount"]}
            </Text.numeral>
          }
          hint={t("markets.symbolInfoBar.24hVolume.tooltip")}
        />
      </React.Suspense>
      <React.Suspense fallback={null}>
        <LazyDataItem
          label={t("markets.symbolInfoBar.predFundingRate")}
          value={<FundingRate symbol={symbol} />}
          hint={<FundingRateHintWidget symbol={symbol} />}
        />
      </React.Suspense>
      <div ref={tailingElementRef}>
        <React.Suspense fallback={null}>
          <LazyDataItem
            label={t("markets.openInterest")}
            value={
              <>
                <Text.numeral rule="human" dp={2}>
                  {openInterest}
                </Text.numeral>
                <Text intensity={36}>{` USDC`}</Text>
              </>
            }
            hint={t("markets.openInterest.tooltip")}
          />
        </React.Suspense>
      </div>
    </Flex>
  );

  return (
    <Flex direction="column" className="oui-h-full oui-w-full" gapY={1}>
      {/* autoHeight: banner text can wrap — fixed 46px row spilled over the chart in compact hosts */}
      <SymbolInfoBarRiskNotice
        visible={isCommunityListed}
        symbolWithBroker={symbolWithBroker}
        brokerName={brokerNameRaw ?? brokerName ?? ""}
        isPreTge={isPreTge}
        autoHeight
      />
      <SymbolInfoBarDesktop
        className={props.className}
        trailing={props.trailing}
        leftSection={leftSection}
        priceSection={priceSection}
        scrollableContent={scrollableContent}
        containerRef={containerRef}
        leadingVisible={leadingVisible}
        tailingVisible={tailingVisible}
        onScroll={onScoll}
      />
      {/* Part 3: RWA countdown */}
      {showCountdown && (
        <RwaCountdown
          closeCountdown={closeCountdown}
          isRwa={isRwa}
          open={open}
          closeTimeInterval={closeTimeInterval}
        />
      )}
    </Flex>
  );
};

type ScrollIndicatorProps = {
  tailing?: boolean;
  leading?: boolean;
  visible?: boolean;
  onClick?: (direction: string) => void;
};

const ScrollIndicator: React.FC<ScrollIndicatorProps> = (props) => {
  const { visible, leading, tailing, onClick } = props;
  if (!visible) {
    return null;
  }

  return (
    <button
      onClick={() => {
        onClick?.(leading ? "left" : "right");
      }}
      style={{
        background:
          "linear-gradient(90deg, rgb(var(--oui-color-base-10)) 0%, rgb(var(--oui-color-base-10) / 0.60) 65%, rgb(var(--oui-color-base-10) / 0.00) 100%)",
      }}
      className={cn(
        "oui-flex oui-w-[80px] oui-items-center",
        "oui-absolute oui-inset-y-0 oui-rounded-s",
        leading && "oui-start-0 oui-ps-1",
        tailing && "oui-end-0 oui-rotate-180 oui-pe-1",
      )}
    >
      <ArrowLeftIcon className="oui-text-base-contrast-54 hover:oui-text-base-contrast-80" />
    </button>
  );
};

const FundingRate: React.FC<{ symbol: string }> = ({ symbol }) => {
  const data = useFundingRate(symbol);

  if (data?.est_funding_rate === null) {
    return "--";
  }

  return (
    <div>
      <Text.numeral unit="%" dp={4} intensity={98}>
        {data.est_funding_rate!}
      </Text.numeral>
      <Text intensity={36} className="oui-tabular-nums">
        {/* not need to translate  */}
        {` in ${data.countDown}`}
      </Text>
    </div>
  );
};

const RwaCountdown: React.FC<{
  closeCountdown?: () => void;
  isRwa: boolean;
  open: boolean | undefined;
  closeTimeInterval: number | undefined;
}> = ({ closeCountdown, isRwa, open, closeTimeInterval }) => {
  const { t } = useTranslation();
  if (
    !isRwa ||
    closeTimeInterval === undefined ||
    open === undefined ||
    closeTimeInterval > 30 * 60
  ) {
    return null;
  }
  if (!open) {
    return null;
  }

  const minutes = closeTimeInterval ? Math.floor(closeTimeInterval / 60) : 0;
  const minutesStr = minutes.toString().padStart(2, "0");
  const seconds = closeTimeInterval ? closeTimeInterval % 60 : 0;
  const secondsStr = seconds.toString().padStart(2, "0");

  return (
    <Flex
      itemAlign="center"
      px={2}
      r="lg"
      className="oui-w-full oui-text-warning-darken oui-bg-warning-darken/15 oui-flex-1 oui-my-2 oui-text-sm"
    >
      <Flex className="oui-w-full oui-pe-2">
        <NewsFillIcon className="oui-text-warning-darken" size={16} />
        <Flex>
          {t("trading.rwa.countdown.title")}
          <Flex className="oui-text-base-contrast-54 oui-text-xs oui-font-normal">
            <Box
              ml={2}
              className="oui-bg-base-7 oui-px-1 oui-rounded-md oui-text-base-contrast oui-min-w-[22px] oui-text-center oui-text-xs"
            >
              {minutesStr}
            </Box>
            {t("common.minuteShort")}
            {" : "}
            <Box
              ml={1}
              className="oui-bg-base-7 oui-px-1 oui-rounded-md oui-text-base-contrast oui-min-w-[22px] oui-text-center oui-text-xs"
            >
              {secondsStr}
            </Box>
            {t("common.secondShort")}
          </Flex>
        </Flex>
      </Flex>

      <button className="oui-cursor-pointer" onClick={closeCountdown}>
        <CloseIcon className="oui-text-base-contrast" size={16} opacity={1} />
      </button>
    </Flex>
  );
};
