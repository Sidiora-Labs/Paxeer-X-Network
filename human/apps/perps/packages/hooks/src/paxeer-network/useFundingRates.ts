import { type API } from "@paxeer-network/types";
import { getTimestamp } from "@paxeer-network/utils";
import { createGetter } from "../utils/createGetter";
import { useAppStore } from "./appStore";

export type FundingRates = ReturnType<typeof useFundingRates>;

export const useFundingRates = () => {
  const data = useAppStore((state) => state.fundingRates);

  return createGetter<API.FundingRate>({ ...data });
};

export const useFundingRatesStore = () => {
  const data = useAppStore((state) => state.fundingRates) as Record<
    string,
    API.FundingRate
  >;
  return data;
};

function getEstFundingRate(data: API.FundingRate) {
  if (!data) return;

  const { next_funding_time, est_funding_rate } = data;

  if (getTimestamp() > next_funding_time) {
    return null;
  }

  return est_funding_rate;
}
