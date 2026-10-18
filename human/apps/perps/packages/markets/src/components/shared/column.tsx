import { ReactNode } from "react";
import { i18n } from "@paxeer-network/i18n";
import { Text, Column, Flex, TokenIcon, Badge, cn } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { FavoritesIcon2, UnFavoritesIcon2 } from "../../icons";
import { DeleteIcon, TopIcon } from "../../icons";
import { FavoriteInstance } from "../../type";
import { FavoritesDropdownMenuWidget } from "../favoritesDropdownMenu";
import { SymbolBadge } from "../symbolBadge";
import { SymbolDisplay } from "../symbolDisplay";

export function getSymbolColumn(
  favorite: FavoriteInstance,
  isFavoriteList = false,
  options?: {
    stackLeverageInSecondRow?: boolean;
    symbolColumnWidth?: number;
  },
) {
  return {
    title: i18n.t("common.symbol"),
    dataIndex: "symbol",
    width: options?.symbolColumnWidth ?? 150,
    className: "oui-z-[1]",
    onSort: true,
    render: (value, record) => {
      let favoritesIcon: ReactNode;
      if (!isFavoriteList) {
        favoritesIcon = (
          <FavoritesDropdownMenuWidget row={record} favorite={favorite}>
            <Flex
              width={12}
              height={12}
              justify="center"
              itemAlign="center"
              className="oui-me-1 oui-cursor-pointer"
            >
              {record.isFavorite ? (
                <FavoritesIcon2 className="oui-size-3" />
              ) : (
                <UnFavoritesIcon2 className="oui-size-3" />
              )}
            </Flex>
          </FavoritesDropdownMenuWidget>
        );
      }

      const stackLeverageInSecondRow = options?.stackLeverageInSecondRow;
      const hasLeverage = typeof record.leverage === "number";
      const showSymbolBadgeInLeverageRow =
        stackLeverageInSecondRow && Boolean(record.broker_id);
      const showSecondRow = hasLeverage || showSymbolBadgeInLeverageRow;

      return (
        <Flex gapX={1} itemAlign="center">
          {favoritesIcon}
          <Flex
            direction={stackLeverageInSecondRow ? "column" : "row"}
            itemAlign={stackLeverageInSecondRow ? "start" : "center"}
            gapY={stackLeverageInSecondRow ? 1 : undefined}
            gapX={stackLeverageInSecondRow ? undefined : 1}
          >
            <Flex gapX={1} itemAlign="center">
              <TokenIcon symbol={value} className="oui-size-[18px]" />
              <SymbolDisplay
                formatString="base"
                size="2xs"
                record={record}
                showBadge={!stackLeverageInSecondRow}
              >
                {value}
              </SymbolDisplay>
            </Flex>
            {stackLeverageInSecondRow
              ? showSecondRow && (
                  <Flex gapX={1} itemAlign="center">
                    {showSymbolBadgeInLeverageRow && (
                      <SymbolBadge symbol={value} />
                    )}
                    {hasLeverage && (
                      <Badge size="xs" color="primary">
                        {record.leverage}x
                      </Badge>
                    )}
                  </Flex>
                )
              : hasLeverage && (
                  <Badge size="xs" color="primary">
                    {record.leverage}x
                  </Badge>
                )}
          </Flex>
        </Flex>
      );
    },
  } as Column<any>;
}

export function getLastColumn() {
  return {
    title: i18n.t("markets.column.last"),
    dataIndex: "24h_close",
    align: "right",
    onSort: true,
    width: 100,
    render: (value, record) => {
      return (
        <Text.numeral dp={record.quote_dp || 2} size="2xs">
          {value}
        </Text.numeral>
      );
    },
  } as Column<any>;
}

export function getMarkColumn() {
  return {
    title: i18n.t("markets.column.mark"),
    dataIndex: "mark_price",
    align: "right",
    onSort: true,
    width: 100,
    render: (value, record) => {
      return (
        <Text.numeral dp={record.quote_dp || 2} size="2xs">
          {value}
        </Text.numeral>
      );
    },
  } as Column<any>;
}

export function getIndexColumn() {
  return {
    title: i18n.t("markets.column.index"),
    dataIndex: "index_price",
    align: "right",
    onSort: true,
    width: 100,
    render: (value, record) => {
      return (
        <Text.numeral dp={record.quote_dp || 2} size="2xs">
          {value}
        </Text.numeral>
      );
    },
  } as Column<any>;
}

export function get24hPercentageColumn() {
  return {
    title: i18n.t("markets.column.24hPercentage"),
    dataIndex: "change",
    align: "right",
    onSort: true,
    width: 80,
    render: (value) => {
      return (
        <Text.numeral
          rule="percentages"
          coloring
          rm={Decimal.ROUND_DOWN}
          showIdentifier
          size="2xs"
        >
          {value}
        </Text.numeral>
      );
    },
  } as Column<any>;
}

export function get24hVolOIColumn() {
  return {
    title: i18n.t("markets.column.24hVol"),
    dataIndex: "24h_amount",
    align: "right",
    className: "oui-h-[36px]",
    width: 100,
    multiSort: {
      fields: [
        {
          sortKey: "24h_amount",
          label: i18n.t("markets.column.24hVol"),
        },
        {
          sortKey: "openInterest",
          label: i18n.t("markets.column.OI"),
        },
      ],
    },
    render: (value, record) => (
      <Flex direction="column" itemAlign="end" gapY={1}>
        <Text.numeral rule="human" dp={2} rm={Decimal.ROUND_DOWN}>
          {value}
        </Text.numeral>
        <Text.numeral
          rule="human"
          dp={2}
          rm={Decimal.ROUND_DOWN}
          size="2xs"
          intensity={54}
        >
          {record.openInterest}
        </Text.numeral>
      </Flex>
    ),
  } as Column<any>;
}

export function getLastAnd24hPercentageColumn(
  favorite: FavoriteInstance,
  isFavoriteList = false,
) {
  return {
    title: i18n.t("markets.column.last&24hPercentage"),
    dataIndex: "change",
    align: "right",
    onSort: true,
    className: "oui-h-[36px]",
    width: 100,
    render: (value, record) => {
      const onDelSymbol: React.MouseEventHandler = (e) => {
        favorite.updateSymbolFavoriteState(
          record,
          favorite.selectedFavoriteTab,
          true,
        );
        e.stopPropagation();
      };

      const iconCls =
        "oui-w-4 oui-h-4 oui-text-base-contrast-54 hover:oui-text-base-contrast";

      const actions = (
        <div
          className={cn(
            "oui-absolute oui-end-0 oui-top-[6.5px]",
            "oui-hidden group-hover:oui-block",
          )}
        >
          <Flex
            className={cn(
              "oui-inline-flex",
              "oui-bg-primary-darken oui-py-[6px]",
            )}
            r="base"
            width={52}
            justify="center"
            itemAlign="end"
            gapX={2}
          >
            <TopIcon
              className={iconCls}
              onClick={(e) => {
                e.stopPropagation();
                favorite.pinToTop(record);
              }}
            />
            <DeleteIcon className={iconCls} onClick={onDelSymbol} />
          </Flex>
        </div>
      );

      return (
        <div className="oui-relative">
          {isFavoriteList && actions}

          <Flex
            direction="column"
            justify="end"
            itemAlign="end"
            gapY={1}
            className={cn(isFavoriteList && "group-hover:oui-invisible")}
          >
            <Text.numeral dp={record.quote_dp || 2} size="2xs">
              {record["24h_close"]}
            </Text.numeral>
            <Text.numeral
              rule="percentages"
              coloring
              rm={Decimal.ROUND_DOWN}
              showIdentifier
              size="2xs"
            >
              {value}
            </Text.numeral>
          </Flex>
        </div>
      );
    },
  } as Column<any>;
}

export function getMarkIndexColumn() {
  return {
    title: i18n.t("markets.column.mark&Index"),
    dataIndex: "mark_price",
    align: "right",
    className: "oui-h-[36px]",
    width: 100,
    multiSort: {
      fields: [
        {
          sortKey: "mark_price",
          label: i18n.t("markets.column.mark"),
        },
        {
          sortKey: "index_price",
          label: i18n.t("markets.column.index"),
        },
      ],
    },
    render: (value, record) => (
      <Flex direction="column" itemAlign="end" gapY={1}>
        <Text.numeral dp={record.quote_dp || 2} size="2xs">
          {value}
        </Text.numeral>
        <Text.numeral dp={record.quote_dp || 2} size="2xs" intensity={54}>
          {record.index_price}
        </Text.numeral>
      </Flex>
    ),
  } as Column<any>;
}
