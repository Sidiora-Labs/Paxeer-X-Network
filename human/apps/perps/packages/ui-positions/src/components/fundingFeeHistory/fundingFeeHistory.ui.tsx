import {
  FC,
  type MutableRefObject,
  useCallback,
  useEffect,
  useMemo,
  useRef,
} from "react";
import { usePrivateInfiniteQuery } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { EMPTY_LIST, MarginMode } from "@paxeer-network/types";
import {
  Grid,
  Statistic,
  DataTable,
  Text,
  ListView,
  Flex,
  useScreen,
  cn,
  Tooltip,
  ExclamationFillIcon,
  modal,
  Spinner,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { SymbolBadge } from "../positions/desktop/symbolBadge";
import { EndReachedBox } from "./endReachedBox";
import { negateFee } from "./negateFee";

type FundingFeeHistory = {
  created_time: number;
  funding_fee: number;
  funding_rate: number;
  mark_price: number;
  payment_type: "Pay" | "Receive";
  status: "Accrued" | "Settled";
  symbol: string;
  margin_mode: MarginMode;
  updated_time: number;
};

const PAGE_SIZE = 60;
const LOAD_MORE_THRESHOLD = 300;

export const FundingFeeHistoryUI: FC<{
  total?: number;
  symbol: string;
  start_t: string;
  end_t: string;
  feeType?: "closed" | "unsettled";
  marginMode?: MarginMode | 0 | 1;
}> = ({ total, symbol, start_t, end_t, feeType = "closed", marginMode }) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const isUnsettled = feeType === "unsettled";
  const feeLabel = t(
    isUnsettled ? "positions.unsettledFundingFee" : "funding.fundingFee",
  );
  const feeTooltip = t(
    isUnsettled
      ? "positions.unsettledFundingFee.tooltip"
      : "positions.fundingFee.tooltip",
  );

  const { isLoading, isValidating, data, setSize } = usePrivateInfiniteQuery<
    FundingFeeHistory[]
  >(
    (pageIndex, previousPageData) => {
      if (
        (!previousPageData || (previousPageData.length ?? 0) < PAGE_SIZE) &&
        pageIndex > 0
      )
        return null;
      return `/v1/funding_fee/history?page=${pageIndex + 1}&size=${PAGE_SIZE}&symbol=${symbol}&start_t=${start_t}&end_t=${end_t}`;
    },
    {
      revalidateFirstPage: false,
    },
  );

  const loadingMoreRef = useRef(false);
  const wasValidatingRef = useRef(isValidating);
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const loadedPageCount = data?.length ?? 0;
  const lastPage = data?.[loadedPageCount - 1];
  const hasMore = Array.isArray(lastPage) && lastPage.length === PAGE_SIZE;

  const loadMore = useCallback(() => {
    if (loadingMoreRef.current || isValidating || !hasMore) return;

    loadingMoreRef.current = true;
    setSize((prev) => {
      return prev + 1;
    });
  }, [hasMore, isValidating, setSize]);

  useEffect(() => {
    if (wasValidatingRef.current && !isValidating) {
      loadingMoreRef.current = false;
    }

    wasValidatingRef.current = isValidating;
  }, [isValidating]);

  const flattenData = useMemo(() => {
    if (!Array.isArray(data)) return [];
    return data
      .flat()
      .filter((item) => marginMode == null || item.margin_mode === marginMode)
      .map((item) => {
        return {
          ...item,
          funding_fee: negateFee(item.funding_fee) ?? 0,
        };
      });
  }, [data, marginMode]);

  useEffect(() => {
    if (isValidating || !hasMore) return;

    const container = scrollContainerRef.current;
    if (!container) return;

    const distanceToBottom =
      container.scrollHeight - container.scrollTop - container.clientHeight;

    if (distanceToBottom <= LOAD_MORE_THRESHOLD) {
      loadMore();
    }
  }, [flattenData.length, hasMore, isValidating, loadMore, loadedPageCount]);

  const isListLoading = isLoading || (flattenData.length === 0 && hasMore);

  const listView = useMemo(() => {
    if (isMobile) {
      return (
        <HistoryDataListViewSimple
          data={flattenData ?? EMPTY_LIST}
          isLoading={isListLoading}
          loadMore={loadMore}
          containerRef={scrollContainerRef}
        />
      );
    }
    return (
      <HistoryDataListView
        data={flattenData ?? EMPTY_LIST}
        isLoading={isListLoading}
        loadMore={loadMore}
        containerRef={scrollContainerRef}
      />
    );
  }, [isMobile, flattenData, isListLoading, loadMore]);

  return (
    <div>
      <Grid
        cols={2}
        gapX={3}
        className="oui-sticky oui-top-0 oui-z-10 oui-bg-base-8 oui-py-4"
      >
        <div className="oui-rounded-lg oui-border oui-border-line-6 oui-bg-base-9 oui-p-3">
          {/* <Statistic label={"Instrument"} /> */}
          <Flex direction={"column"} gap={1} itemAlign={"start"}>
            <span className="oui-text-2xs oui-text-base-contrast-36">
              {t("common.symbol")}
            </span>
            <Text.formatted
              rule="symbol"
              formatString="base"
              className="oui-font-semibold"
              intensity={98}
              suffix={<SymbolBadge symbol={symbol} />}
            >
              {symbol}
            </Text.formatted>
          </Flex>
        </div>
        <div className="oui-rounded-lg oui-border oui-border-line-6 oui-bg-base-9 oui-p-3">
          <Statistic
            label={
              isMobile ? (
                <FundingFeeLabelButton
                  label={`${feeLabel} (USDC)`}
                  tooltip={feeTooltip}
                  size={14}
                />
              ) : (
                <FundingFeeLabel
                  label={`${feeLabel} (USDC)`}
                  tooltip={feeTooltip}
                  size={14}
                />
              )
            }
            valueProps={{
              ignoreDP: true,
              coloring: true,
              showIdentifier: true,
            }}
          >
            {total ?? "--"}
          </Statistic>
        </div>
      </Grid>
      {listView}
    </div>
  );
};

const FundingFeeLabelButton: FC<{
  label: string;
  tooltip?: string;
  size: number;
}> = ({ label, tooltip, size }) => {
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-items-center oui-gap-1">
      <span>{label}</span>
      {tooltip && (
        <button
          className="oui-flex oui-items-center"
          onClick={() => {
            modal.alert({
              message: tooltip,
              title: t("positions.fundingFee.title"),
            });
          }}
        >
          <ExclamationFillIcon
            className="oui-cursor-pointer oui-text-base-contrast-54"
            size={size}
          />
        </button>
      )}
    </div>
  );
};

const FundingFeeLabel: FC<{
  label: string;
  tooltip?: string;
  size: number;
}> = ({ label, tooltip, size }) => {
  return (
    <div className="oui-flex oui-items-center oui-gap-1">
      <span>{label}</span>
      {tooltip && (
        <Tooltip content={<div className="oui-w-64">{tooltip}</div>}>
          <ExclamationFillIcon
            className="oui-cursor-pointer oui-text-base-contrast-54"
            size={size}
          />
        </Tooltip>
      )}
    </div>
  );
};

type ListProps = {
  isLoading: boolean;
  data: any[];
  loadMore: () => void;
  containerRef: MutableRefObject<HTMLDivElement | null>;
};

const HistoryDataListView: FC<ListProps> = ({
  isLoading,
  data,
  loadMore,
  containerRef,
}) => {
  const { t } = useTranslation();
  const columns = useMemo(() => {
    return [
      {
        title: t("common.time"),
        dataIndex: "created_time",
        width: 175,
        render: (value: number) => {
          return <Text.formatted rule="date">{value}</Text.formatted>;
        },
      },
      {
        title: <FundingFeeLabel label={t("funding.fundingRate")} size={12} />,
        dataIndex: "funding_rate",
        formatter: (value: string) => new Decimal(value).mul(100).toString(),
        render: (value: string) => {
          return <span>{`${value}%`}</span>;
        },
      },
      {
        title: t("funding.paymentType"),
        dataIndex: "payment_type",
        formatter: (value: string) => {
          return value === "Pay"
            ? t("funding.paymentType.paid")
            : t("funding.paymentType.received");
        },
        render: (value: string) => <span>{value}</span>,
      },
      {
        title: (
          <FundingFeeLabel
            label={t("common.amount")}
            tooltip={t("positions.fundingRate.tooltip")}
            size={12}
          />
        ),
        dataIndex: "funding_fee",
        render: (value: string) => {
          return (
            <Text.numeral rule="price" coloring showIdentifier ignoreDP>
              {value}
            </Text.numeral>
          );
        },
      },
    ];
  }, [t]);

  return (
    <div
      ref={containerRef}
      className="oui-custom-scrollbar oui-h-[calc(80vh_-_132px_-_8px)] oui-overflow-y-auto"
    >
      <EndReachedBox onEndReached={loadMore}>
        <DataTable
          classNames={{
            root: cn("oui-h-auto oui-bg-base-8 oui-text-sm"),
          }}
          columns={columns}
          dataSource={data ?? EMPTY_LIST}
          loading={isLoading}
        />
      </EndReachedBox>
    </div>
  );
};

const HistoryDataListViewSimple: FC<ListProps> = ({
  data,
  isLoading,
  loadMore,
  containerRef,
}) => {
  const renderItem = useCallback((item: FundingFeeHistory) => {
    return <FundingFeeItem item={item} />;
  }, []);
  return (
    <div
      ref={containerRef}
      className="oui-custom-scrollbar oui-h-[calc(80vh_-_104px)] oui-overflow-y-auto"
    >
      <ListView
        dataSource={data}
        renderItem={renderItem}
        isLoading={isLoading}
        emptyView={
          isLoading ? (
            <div className="oui-flex oui-h-full oui-items-center oui-justify-center">
              <Spinner />
            </div>
          ) : undefined
        }
        contentClassName="oui-space-y-0"
        loadMore={loadMore}
      />
    </div>
  );
};

const FundingFeeItem: FC<{
  item: any;
}> = ({ item }) => {
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-flex-col oui-space-y-2 oui-border-t oui-border-line-6 oui-py-2">
      <Flex justify={"between"}>
        <Statistic
          label={
            <FundingFeeLabelButton label={t("funding.fundingRate")} size={12} />
          }
          classNames={{
            label: "oui-text-2xs",
          }}
          valueProps={{
            ignoreDP: true,
            as: "div",
            className: "oui-text-xs",
            suffix: "%",
          }}
        >
          {new Decimal(item.funding_rate).mul(100).toNumber()}
        </Statistic>
        <Statistic
          label={
            <FundingFeeLabelButton
              label={t("common.amount")}
              tooltip={t("positions.fundingRate.tooltip")}
              size={12}
            />
          }
          className="oui-items-end"
          classNames={{
            label: "oui-text-2xs",
          }}
          valueProps={{
            ignoreDP: true,
            coloring: true,
            as: "div",
            className: "oui-text-xs",
            showIdentifier: true,
          }}
        >
          {item.funding_fee}
        </Statistic>
      </Flex>
      <Flex justify={"between"}>
        <Text.formatted
          rule="date"
          className="oui-text-base-contrast-36"
          size="2xs"
        >
          {item.created_time}
        </Text.formatted>
        <Text size="sm" intensity={80}>
          {item.payment_type === "Pay"
            ? t("funding.paymentType.paid")
            : t("funding.paymentType.received")}
        </Text>
      </Flex>
    </div>
  );
};
