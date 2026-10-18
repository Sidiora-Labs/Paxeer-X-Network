import { format, subDays } from "date-fns";
import type { VolChartDataItem } from "@paxeer-network/chart";

export function fillData(
  days: number,
  origin?: VolChartDataItem[],
): VolChartDataItem[] {
  const now = Date();
  const result = new Array(days)
    .fill(0)
    .map<VolChartDataItem>((_, index) => {
      return {
        date: format(subDays(now, index + 1), "yyyy-MM-dd"),
        volume: 0,
        opacity: 0,
      };
    })
    .reverse();

  const dataObject = origin?.reduce<{ [key: string]: VolChartDataItem }>(
    (acc, curr) => {
      acc[curr.date] = curr;
      return acc;
    },
    {},
  );

  for (let index = 0; index < result.length; index++) {
    const element = result[index];
    const originData = dataObject?.[element.date];
    if (originData) {
      result[index] = { ...originData, opacity: originData.volume > 0 ? 1 : 0 };
    }
  }

  return result;
}
