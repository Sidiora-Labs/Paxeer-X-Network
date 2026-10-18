import { API } from "@paxeer-network/types";
import {
  getTPSLEditableOrderValues,
  TPSLEditableOrderValues,
} from "../positionTPSL/tpslOrderSync";

export type BracketTPSLPriceInfo = Omit<TPSLEditableOrderValues, "quantity">;

const priceKeys = [
  "tp_trigger_price",
  "tp_order_price",
  "sl_trigger_price",
  "sl_order_price",
] as const satisfies readonly (keyof BracketTPSLPriceInfo)[];

const normalizePrice = (value: number | string | null | undefined) => {
  if (value === undefined || value === null || value === "") return "";
  const number = Number(value);
  return Number.isFinite(number) ? number : String(value);
};

export function getBracketTPSLPriceInfo(
  order: API.AlgoOrder,
): BracketTPSLPriceInfo {
  const { quantity: _, ...priceInfo } = getTPSLEditableOrderValues(order);
  return priceInfo;
}

export function hasBracketTPSLPriceChanged(
  initial: BracketTPSLPriceInfo,
  current: Partial<BracketTPSLPriceInfo>,
): boolean {
  return priceKeys.some(
    (key) => normalizePrice(initial[key]) !== normalizePrice(current[key]),
  );
}
