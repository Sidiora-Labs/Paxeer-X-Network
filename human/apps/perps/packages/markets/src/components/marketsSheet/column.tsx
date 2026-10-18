import { Column } from "@paxeer-network/ui";
import { FavoriteInstance } from "../../type";
import {
  get24hVolOIColumn,
  getLastAnd24hPercentageColumn,
  getSymbolColumn,
} from "../shared/column";

export const getMarketsSheetColumns = (
  favorite: FavoriteInstance,
  isFavoriteList = false,
) => {
  return [
    getSymbolColumn(favorite, isFavoriteList, {
      stackLeverageInSecondRow: true,
    }),
    get24hVolOIColumn(),
    getLastAnd24hPercentageColumn(favorite, isFavoriteList),
  ] as Column[];
};
