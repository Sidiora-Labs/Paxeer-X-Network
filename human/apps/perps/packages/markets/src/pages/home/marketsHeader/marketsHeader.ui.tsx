import { FC, ReactNode, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, Flex, Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useMarketsContext } from "../../../components/marketsProvider";
import { SymbolDisplay } from "../../../components/symbolDisplay";
import { PaxeerNetworkIcon } from "../../../icons";
import { MarketsHeaderReturns } from "./marketsHeader.script";

/** -----------MarketsHeader start ------------ */
type MarketsHeaderProps = MarketsHeaderReturns & {
  className?: string;
};
export const MarketsHeader: FC<MarketsHeaderProps> = (props) => {
  const {
    news,
    gainers,
    losers,
    total24Amount,
    totalOpenInterest,
    tvl,
    favorite,
  } = props;
  const { onSymbolChange } = useMarketsContext();
  const { t } = useTranslation();

  const onSymbol = (item: any) => {
    onSymbolChange?.(item);
    favorite.addToHistory(item);
  };

  return (
    <div
      id="oui-markets-header"
      className={cn("oui-overflow-hidden", props.className)}
    >
      <div className="oui-grid oui-grid-cols-3 oui-gap-4">
        <BlockList
          total24Amount={total24Amount}
          totalOpenInterest={totalOpenInterest}
          tvl={tvl}
        />
        <CardItem
          data={news}
          title={
            <Text.gradient color="brand">
              {t("markets.newListings")}
            </Text.gradient>
          }
          onSymbol={onSymbol}
        />
        <CardItem
          data={gainers}
          title={
            <Text className="oui-text-success-light">
              {t("markets.topGainers")}
            </Text>
          }
          onSymbol={onSymbol}
        />
        <CardItem
          data={losers}
          title={
            <Text className="oui-text-danger-light">
              {t("markets.topLosers")}
            </Text>
          }
          onSymbol={onSymbol}
        />
      </div>
    </div>
  );
};
/** -----------MarketsHeader end ------------ */

type BlockListProps = {
  className?: string;
  total24Amount?: number;
  totalOpenInterest?: number;
  tvl?: number;
};

/** -----------MarketsHeader start ------------ */
const BlockList: React.FC<BlockListProps> = (props) => {
  const { total24Amount, totalOpenInterest, tvl } = props;
  const { t } = useTranslation();

  const list = useMemo(() => {
    return [
      {
        label: (
          <Flex gapX={1}>
            <PaxeerNetworkIcon /> {t("markets.column.24hVolume")}
          </Flex>
        ),
        value: total24Amount,
      },
      {
        label: (
          <Flex gapX={1}>
            <PaxeerNetworkIcon /> {t("markets.openInterest")}
          </Flex>
        ),
        value: totalOpenInterest,
      },
      {
        label: (
          <Flex gapX={1}>
            <PaxeerNetworkIcon /> {`${t("common.assets")} (TVL)`}
          </Flex>
        ),
        value: tvl,
      },
    ];
  }, [t, total24Amount, totalOpenInterest, tvl]);

  return (
    <div
      className={cn(
        "oui-col-span-3 oui-grid oui-grid-cols-3 oui-gap-4",
        props.className,
      )}
    >
      {list?.map((item, index) => (
        <BlockItem key={`item-${index}`} {...item} />
      ))}
    </div>
  );
};
/** -----------MarketsHeader start ------------ */

type BlockItemProps = {
  label: ReactNode;
  value?: number;
  rule?: string;
  dp?: number;
};

const BlockItem: React.FC<BlockItemProps> = (props) => {
  return (
    <Box
      r="lg"
      className={cn(
        "oui-flex oui-w-full oui-flex-col oui-justify-between",
        "oui-min-h-[clamp(6.5rem,8vw,8.25rem)] oui-p-[clamp(1rem,1.75vw,1.75rem)]",
        "oui-bg-primary-darken oui-text-primary-contrast",
      )}
    >
      <Text
        as="div"
        weight="semibold"
        className="oui-text-[length:clamp(0.75rem,0.9vw,0.875rem)] oui-leading-5 oui-text-primary-contrast/80"
      >
        {props.label}
      </Text>

      <Text.numeral
        currency="$"
        dp={props.dp || 0}
        rm={Decimal.ROUND_DOWN}
        rule={props.rule as any}
        className={cn(
          "oui-text-[length:clamp(1.5rem,2.25vw,2.75rem)] oui-font-semibold",
          "oui-tabular-nums oui-tracking-[-0.025em] oui-leading-none",
        )}
      >
        {props.value!}
      </Text.numeral>
    </Box>
  );
};

type CardItemProps = {
  data?: TListItem[];
  title: ReactNode;
  className?: string;
  onSymbol: (item: any) => void;
};

const CardItem: React.FC<CardItemProps> = (props) => {
  return (
    <Box intensity={900} r="lg" py={4} pb={2} className={props.className}>
      <Box px={4}>
        <Text.gradient color="brand" size="sm" weight="semibold">
          {props.title}
        </Text.gradient>
      </Box>

      <Flex direction="column" itemAlign="start" mt={2}>
        {props.data?.map((item, index) => (
          <ListItem key={item.symbol} item={item} onSymbol={props.onSymbol} />
        ))}
      </Flex>
    </Box>
  );
};

type TListItem = {
  symbol: string;
  price: string;
  change: number;
  precision: number;
  [x: string]: any;
};

type ListItemProps = {
  item: TListItem;
  className?: string;
  onSymbol: (item: any) => void;
};

const ListItem: React.FC<ListItemProps> = (props) => {
  const { item } = props;

  return (
    <Box
      width="100%"
      py={2}
      px={4}
      className={cn(
        "oui-grid oui-grid-cols-[minmax(0,1fr)_96px_72px] oui-items-center oui-gap-x-3",
        "oui-cursor-pointer hover:oui-bg-base-8",
        props.className,
      )}
      onClick={() => {
        props.onSymbol(item);
      }}
    >
      <SymbolDisplay formatString="base" showIcon className="oui-min-w-0">
        {item.symbol}
      </SymbolDisplay>

      <Flex justify="end">
        <Text.numeral
          currency="$"
          size="xs"
          weight="semibold"
          dp={item.quote_dp}
          className="oui-tabular-nums oui-text-end"
        >
          {item["24h_close"]}
        </Text.numeral>
      </Flex>

      <Flex justify="end">
        <Text.numeral
          rule="percentages"
          coloring
          size="xs"
          weight="semibold"
          showIdentifier
          className="oui-tabular-nums oui-text-end"
        >
          {item.change}
        </Text.numeral>
      </Flex>
    </Box>
  );
};
