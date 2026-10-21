import { Resolution } from '@blockscout/stats-types';
import type { StatsIntervalIds } from 'types/client/stats';

export const STATS_RESOLUTIONS: Array<{ id: Resolution; title: string }> = [
  {
    id: Resolution.DAY,
    title: 'Day',
  },
  {
    id: Resolution.WEEK,
    title: 'Week',
  },
  {
    id: Resolution.MONTH,
    title: 'Month',
  },
  {
    id: Resolution.YEAR,
    title: 'Year',
  },
];

export const STATS_INTERVALS: { [key in StatsIntervalIds]: { title: string; shortTitle: string; start?: Date } } = {
  all: {
    title: 'All time',
    shortTitle: 'All time',
  },
  oneMonth: {
    title: '1 month',
    shortTitle: '1M',
    start: getStartDateInPast(1),
  },
  threeMonths: {
    title: '3 months',
    shortTitle: '3M',
    start: getStartDateInPast(3),
  },
  sixMonths: {
    title: '6 months',
    shortTitle: '6M',
    start: getStartDateInPast(6),
  },
  oneYear: {
    title: '1 year',
    shortTitle: '1Y',
    start: getStartDateInPast(12),
  },
};

function getStartDateInPast(months: number): Date {
  const date = new Date();
  date.setMonth(date.getMonth() - months);
  return date;
}

export interface StatsSectionKey {
  id: string;
  title: string;
}

// The counters grid sits above the chart sections and is reached by the same section navigation,
// so it carries a section key of its own beside the keys the charts service names.
export const STATS_OVERVIEW_SECTION: StatsSectionKey = {
  id: 'overview',
  title: 'Overview stats',
};

// A section anchor is the section id itself: the charts page is linked with that hash from the gas
// tracker and from the chart pages, and those links have to keep landing on the section.
export function getStatsSectionAnchorId(sectionId: string): string {
  return sectionId;
}

export function getStatsSectionKeys(sections?: Array<StatsSectionKey>): Array<StatsSectionKey> {
  return [ STATS_OVERVIEW_SECTION, ...(sections ?? []).map(({ id, title }) => ({ id, title })) ];
}
