import { FC, useCallback } from "react";
import { useConfig } from "@paxeer-network/hooks";
import {
  cn,
  DataTable,
  Flex,
  Spinner,
  TanstackColumn,
  useScreen,
} from "@paxeer-network/ui";
import { type CampaignRankingData } from "../campaignRanking/campaignRanking.script";
import {
  type GeneralRankingData,
  GeneralRankingScriptReturn,
} from "../generalRanking/generalRanking.script";
import { type RankingColumnFields, useRankingColumns } from "./column";
import { getCurrentAddressRowKey } from "./util";

type RankingData = GeneralRankingData | CampaignRankingData;

export type RankingProps = {
  style?: React.CSSProperties;
  className?: string;
  fields: RankingColumnFields[];
} & Omit<GeneralRankingScriptReturn, "dataList" | "dataSource"> & {
    dataList: RankingData[];
    dataSource: RankingData[];
    type?: "general" | "campaign";
  };

export const Ranking: FC<RankingProps> = (props) => {
  const column = useRankingColumns(
    props.fields,
    props.address,
    typeof props.onSort === "function",
    props.type,
  );
  const { isMobile } = useScreen();

  const onRow = useCallback(
    (record: RankingData, index: number) => {
      const isYou = record.key === getCurrentAddressRowKey(props.address!);
      const isFirst = record.rank === 1;
      const isSecond = record.rank === 2;
      const isThird = record.rank === 3;

      const showBg = isFirst || isSecond || isThird;

      return {
        className: cn(
          "oui-h-[40px] md:oui-h-[48px]",
          // use oui-relative to let the background image position based on row
          "oui-relative",
          isYou
            ? // add 4px extra height to make row has 2px space
              "oui-h-[44px] md:oui-h-[52px]"
            : cn(
                showBg && "oui-border-b-2 oui-border-b-transparent",
                isFirst &&
                  "oui-bg-[linear-gradient(270deg,rgba(241,215,121,0.0225)_-2.05%,rgba(255,203,70,0.45)_100%)]",
                isSecond &&
                  "oui-bg-[linear-gradient(270deg,rgba(255,255,255,0.0225)_-2.05%,rgba(199,199,199,0.45)_100%)]",
                isThird &&
                  "oui-bg-[linear-gradient(270deg,rgba(255,233,157,0.0225)_-1.3%,rgba(160,101,46,0.45)_100%)]",
              ),
        ),
      };
    },
    [props.address],
  );

  const onCell = useCallback(
    (
      column: TanstackColumn<RankingData>,
      record: RankingData,
      index: number,
    ) => {
      const isFirstColumn = column.getIsFirstColumn();
      const isLastColumn = column.getIsLastColumn();
      const isRank = [1, 2, 3].includes(record.rank as number);
      if (record.key === getCurrentAddressRowKey(props.address!)) {
        return {
          className: cn(
            "after:oui-absolute after:oui-h-[40px] after:oui-w-full md:after:oui-h-[48px]",
            "after:oui-border-[rgb(var(--oui-gradient-brand-start))]",
            "after:oui-start-0 after:oui-top-[2px] after:oui-z-[-1]",
            "after:oui-border-y",
            isFirstColumn && "after:oui-rounded-s-lg after:oui-border-s",
            isLastColumn && "after:oui-rounded-e-lg after:oui-border-e",
          ),
        };
      }
      return {
        className: cn(
          isFirstColumn &&
            isRank &&
            "oui-rounded-s-lg oui-mix-blend-luminosity",
          isLastColumn && isRank && "oui-rounded-e-lg oui-mix-blend-luminosity",
        ),
        // style: isRank
        //   ? {
        //       position: "unset",
        //     }
        //   : undefined,
      };
    },
    [props.address],
  );

  if (isMobile) {
    return (
      <>
        <DataTable
          classNames={{
            root: "oui-trading-leaderboard-ranking-table",
            body: "oui-text-2xs",
            scroll: "oui-overflow-y-hidden oui-h-full",
          }}
          loading={props.isLoading}
          columns={column}
          bordered
          initialSort={props.initialSort}
          onSort={props.onSort}
          dataSource={props.dataList}
          generatedRowKey={(record: RankingData) =>
            record.key || record.address
          }
          manualPagination
          manualSorting
          onRow={onRow}
          onCell={onCell}
        />
        <div
          ref={props.sentinelRef}
          className="oui-invisible oui-relative oui-top-[-300px] oui-h-px"
        />
        {props.isLoading && props.dataList.length > 0 && (
          <Flex itemAlign="center" justify="center" width="100%" height={40}>
            <Spinner size="sm" />
          </Flex>
        )}
      </>
    );
  }

  return (
    <DataTable
      loading={props.isLoading}
      columns={column}
      initialSort={props.initialSort}
      onSort={props.onSort}
      bordered
      dataSource={props.dataSource}
      generatedRowKey={(record: RankingData) => record.key || record.address}
      // manualPagination
      manualSorting
      // pagination={props.pagination}
      classNames={{
        root: cn(
          "oui-trading-leaderboard-ranking-table",
          "!oui-h-[calc(100%_-_53px_-_8px)]",
        ),
        scroll: "oui-min-h-[600px] oui-max-h-[1250px]",
      }}
      onRow={onRow}
      onCell={onCell}
    />
  );
};
