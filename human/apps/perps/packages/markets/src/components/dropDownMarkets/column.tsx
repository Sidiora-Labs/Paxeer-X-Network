import { MouseEventHandler, useCallback } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Flex, Text, cn, Column } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { DeleteIcon, TopIcon } from "../../icons";
import { FavoriteInstance } from "../../type";
import {
  get24hPercentageColumn,
  getLastColumn,
  getSymbolColumn,
} from "../shared/column";

export const useDropDownMarketsColumns = () => {
  const { t } = useTranslation();

  return useCallback(
    (favorite: FavoriteInstance, isFavoriteList = false) => {
      return [
        getSymbolColumn(favorite, isFavoriteList, {
          symbolColumnWidth: 190,
        }),
        getLastColumn(),
        get24hPercentageColumn(),
        {
          title: t("markets.column.24hVol"),
          dataIndex: "24h_amount",
          align: "right",
          onSort: true,
          className: "oui-relative",
          width: 80,
          render: (value, record) => {
            const onDelSymbol: MouseEventHandler = (e) => {
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
              <div className={cn("oui-absolute oui-end-1 oui-top-[3px]")}>
                <Flex
                  className={cn(
                    "oui-bg-primary-darken oui-py-[6px]",
                    "oui-hidden group-hover:oui-inline-flex",
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
              <>
                {isFavoriteList && actions}
                <Text.numeral
                  rule="human"
                  dp={2}
                  rm={Decimal.ROUND_DOWN}
                  className={cn(isFavoriteList && "group-hover:oui-invisible")}
                >
                  {value}
                </Text.numeral>
              </>
            );
          },
        },
        {
          title: t("markets.openInterest"),
          dataIndex: "openInterest",
          align: "right",
          onSort: true,
          width: 80,
          render: (value) => (
            <Text.numeral
              rule="human"
              dp={2}
              rm={Decimal.ROUND_DOWN}
              size="2xs"
            >
              {value}
            </Text.numeral>
          ),
        },
      ] as Column[];
    },
    [t],
  );
};
