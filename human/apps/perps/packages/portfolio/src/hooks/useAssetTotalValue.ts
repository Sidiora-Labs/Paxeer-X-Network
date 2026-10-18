import { useMemo } from "react";
import {
  useAccount,
  useCollateral,
  useIndexPricesStream,
} from "@paxeer-network/hooks";
import { SubAccount } from "@paxeer-network/hooks";
import { Decimal } from "@paxeer-network/utils";
import { useAccountsData } from "./useAccountsData";

const isNumber = (val: unknown): val is number => {
  return typeof val === "number" && !Number.isNaN(val);
};

export const calculateTotalHolding = (
  data: SubAccount[] | SubAccount["holding"],
  getIndexPrice: (token: string) => number,
) => {
  let total = new Decimal(0);
  for (const item of data) {
    if (Array.isArray(item.holding)) {
      for (const hol of item.holding) {
        if (isNumber(hol.holding)) {
          const indexPrice = getIndexPrice(hol.token);
          total = total.plus(new Decimal(hol.holding).mul(indexPrice));
        }
      }
    } else if (isNumber(item.holding) && "token" in item) {
      const indexPrice = getIndexPrice(item.token);
      total = total.plus(new Decimal(item.holding).mul(indexPrice));
    }
  }
  return total;
};

export const useAssetTotalValue = () => {
  const { state, isMainAccount } = useAccount();
  const { holding = [] } = useCollateral();
  const { getIndexPrice } = useIndexPricesStream();
  const allAccounts = useAccountsData();

  const subAccounts = state.subAccounts ?? [];

  // Calculate main account total value
  const mainTotalValue = useMemo<Decimal>(
    () => calculateTotalHolding(holding, getIndexPrice),
    [holding, getIndexPrice],
  );

  // Calculate sub accounts total value
  const subTotalValue = useMemo<Decimal>(
    () => calculateTotalHolding(subAccounts, getIndexPrice),
    [subAccounts, getIndexPrice],
  );

  // Calculate final total value
  const totalValue = useMemo<number>(() => {
    if (isMainAccount) {
      return mainTotalValue.plus(subTotalValue).toNumber();
    } else {
      const find = allAccounts.find((item) => item.id === state.accountId);
      if (Array.isArray(find?.children)) {
        return calculateTotalHolding(find.children, getIndexPrice).toNumber();
      }
      return 0;
    }
  }, [
    isMainAccount,
    mainTotalValue,
    subTotalValue,
    allAccounts,
    state.accountId,
    getIndexPrice,
  ]);

  return totalValue;
};
