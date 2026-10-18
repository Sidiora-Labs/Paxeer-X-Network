import { format, toDate } from "date-fns";
import { i18n } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";

// import { toZonedTime } from "date-fns-tz";
// const { toZonedTime } = require("date-fns-tz");

export function generateReferralLink(referralLinkUrl: string, code: string) {
  return `${referralLinkUrl || window?.location?.origin}?ref=${code}`;
}

export async function copyText(content: string) {
  try {
    await navigator.clipboard.writeText(content);
    toast.success(i18n.t("common.copy.copied"));
  } catch (error) {
    toast.success(i18n.t("common.copy.failed"));
  }
}

export function parseTime(time?: number | string): Date | null {
  if (!time) {
    return null;
  }
  const timestamp = typeof time === "number" ? time : Date.parse(time);

  if (!isNaN(timestamp)) {
    return new Date(timestamp);
  }

  return null;
}

//** will be return YYYY-MM-ddThh:mm:ssZ */
export function formatDateTimeToUTC(input?: number | string): string {
  if (input === undefined) {
    return "";
  }
  const date = toDate(input);
  const utcDate = toUTCDate(date);
  return format(utcDate, "yyyy-MM-dd HH:mm:ss 'UTC'");
}

//** will return yyyy-MM-dd */
export function formatYMDTime(time?: number | string): string | undefined {
  if (time === undefined) {
    return undefined;
  }
  const date = toDate(time);
  const utcDate = toUTCDate(date);
  return format(utcDate, "yyyy-MM-dd");
}

//** will return hh:mm */
export function formatHMTime(time?: number | string): string | undefined {
  if (time === undefined) {
    return undefined;
  }
  const date = toDate(time);
  const utcDate = toUTCDate(date);
  return format(utcDate, "hh:mm");
}

//** will return MM-dd */
export function formatMdTime(time?: number | string): string | undefined {
  if (time === undefined) {
    return undefined;
  }
  const date = toDate(time);
  const utcDate = toUTCDate(date);
  return format(utcDate, "MM-dd");
}

//** compare two date, yyyy-mm-dd */
export function compareDate(d1?: Date, d2?: Date) {
  const isEqual =
    d1 &&
    d2 &&
    d1.toISOString().substring(0, 10) === d2.toISOString().substring(0, 10);

  return isEqual;
}

export function generateData(
  itemCount: number,
  data: any[] | null | undefined,
  timeKey: string,
  valueKey: string,
): [string, number][] {
  const result: [string, number][] = [];

  for (let i = 0; i < itemCount; i++) {
    const currentDate = new Date();
    currentDate.setDate(currentDate.getDate() - i - 1);
    const currentDateStr = currentDate.toISOString().substring(0, 10);

    const matchedData = data?.find((item) => {
      const itemDate = parseTime(item[timeKey]);
      if (!itemDate) {
        return false;
      }
      return itemDate.toISOString().substring(0, 10) === currentDateStr;
    });

    if (matchedData) {
      result.push([currentDateStr, matchedData[valueKey]]);
    } else {
      result.push([currentDateStr, 0]);
    }
  }

  return result.reverse();
}

function toUTCDate(date: Date): Date {
  return new Date(date.toUTCString());
}
