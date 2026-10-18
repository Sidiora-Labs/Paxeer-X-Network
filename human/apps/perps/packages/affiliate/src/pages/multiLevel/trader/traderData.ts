import { addDays } from "date-fns";
import { Decimal } from "@paxeer-network/utils";

export type RebateRow = {
  date: string;
  referee_rebate: number;
};

export type VolumeRow = {
  date: string;
  perp_volume: number;
};

export type RebateTableRow = RebateRow & { volume?: number };

const DAY_MS = 86_400_000;

export function utcYesterday(now = new Date()): string {
  return new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - 1),
  )
    .toISOString()
    .slice(0, 10);
}

export function utcDayWindow(days: number, endDate = utcYesterday()) {
  const end = Date.parse(`${endDate}T00:00:00Z`);
  return {
    startDate: new Date(end - (days - 1) * DAY_MS).toISOString().slice(0, 10),
    endDate,
  };
}

export function calendarDate(date: string): Date {
  const [year, month, day] = date.split("-").map(Number);
  return new Date(year, month - 1, day);
}

export function calendarDateString(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

export function calendarDayDateRange(endDate: string, days: number) {
  const to = calendarDate(endDate);
  const from = addDays(to, -(days - 1));

  return { from, to };
}

export function resolveRebateRows(
  multiRows?: RebateRow[],
  multiError?: unknown,
  legacyRows?: RebateRow[],
  legacyError?: unknown,
): { rows?: RebateRow[]; error?: unknown } {
  if (multiRows?.length) return { rows: multiRows };
  if (legacyRows?.length) return { rows: legacyRows };
  if (multiError) return { error: multiError };
  if (legacyError) return { error: legacyError };
  if (multiRows && legacyRows) return { rows: [] };
  return {};
}

export function sumRebates(rows: RebateRow[]): number {
  return rows
    .reduce((sum, row) => sum.add(row.referee_rebate), new Decimal(0))
    .toNumber();
}

export function sumVolume(rows: VolumeRow[]): number {
  return rows
    .reduce((sum, row) => sum.add(row.perp_volume), new Decimal(0))
    .toNumber();
}

export function rebateChartData(
  rows: RebateRow[],
  days: number,
  endDate: string,
) {
  const values = new Map<string, Decimal>();
  rows.forEach((row) => {
    values.set(
      row.date,
      (values.get(row.date) ?? new Decimal(0)).add(row.referee_rebate),
    );
  });

  const end = Date.parse(`${endDate}T00:00:00Z`);
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(end - (days - index - 1) * DAY_MS)
      .toISOString()
      .slice(0, 10);
    return { date, volume: values.get(date)?.toNumber() ?? 0 };
  });
}

export function joinRebateVolume(
  rows: RebateRow[],
  volumeRows: VolumeRow[],
): RebateTableRow[] {
  const volumes = new Map(volumeRows.map((row) => [row.date, row.perp_volume]));
  return rows
    .map((row) => ({ ...row, volume: volumes.get(row.date) }))
    .sort((a, b) => b.date.localeCompare(a.date));
}
