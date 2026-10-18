import { useTranslation } from "@paxeer-network/i18n";
import { Column } from "@paxeer-network/ui";
import type { FavoriteInstance } from "../../type";
import {
  get24hVolOIColumn,
  getLastAnd24hPercentageColumn,
  getSymbolColumn,
} from "../shared/column";

export const useSideMarketsColumns = (
  favorite: FavoriteInstance,
  isFavoriteList = false,
) => {
  // Column factories below still call i18n.t internally, so subscribe here.
  useTranslation();
  const symbolCol = getSymbolColumn(favorite, isFavoriteList, {
    stackLeverageInSecondRow: true,
  });
  const volOiCol = get24hVolOIColumn();
  const lastPctCol = getLastAnd24hPercentageColumn(favorite, isFavoriteList);

  return [
    {
      ...symbolCol,
      width: 115,
    },
    volOiCol,
    lastPctCol,
  ] as Column[];
};
