import { max, min } from "ramda";
import { getTimestamp } from "@paxeer-network/utils";
import { usePrivateQuery } from "../usePrivateQuery";
import { RefferalAPI } from "./api";
import { formatDate } from "./format";

export const useDaily = (options?: {
  //** default Date() - 30d */
  startDate?: Date;
  //** default Date() */
  endDate?: Date;
}): {
  data?: RefferalAPI.DayliVolume[];
  mutate: any;
} => {
  const path = "/v1/volume/user/daily";
  const endDate = options?.startDate || new Date();
  const startDate =
    options?.endDate || new Date(getTimestamp() - 86400000 * 30);

  const start_date = formatDate(startDate);
  const end_date = formatDate(endDate);

  const url = `${path}?start_date=${min(start_date, end_date)}&end_date=${max(
    start_date,
    end_date,
  )}`;
  const { data: dailyVolume, mutate } = usePrivateQuery<
    RefferalAPI.DayliVolume[]
  >(url, {
    revalidateOnFocus: false,
  });

  return {
    data: dailyVolume,
    mutate,
  };
};
