import type { TimeChartItem } from 'toolkit/components/charts/types';

export function formatDate(date: Date) {
  return date.toISOString().substring(0, 10);
}

export const sortByDateDesc = (a: Pick<TimeChartItem, 'date'>, b: Pick<TimeChartItem, 'date'>) => {
  return a.date.getTime() - b.date.getTime();
};

export type ChartColorRole = 'line' | 'areaStart' | 'areaStop' | 'bar' | 'axis' | 'grid';

export type ChartColorMode = 'light' | 'dark';

interface ChartColorToken {
  light: string;
  dark: string;
}

export const CHART_COLOR_TOKENS: Record<ChartColorRole, ChartColorToken> = {
  line: {
    light: 'theme.graph.line._light',
    dark: 'theme.graph.line._dark',
  },
  areaStart: {
    light: 'theme.graph.gradient.start._light',
    dark: 'theme.graph.gradient.start._dark',
  },
  areaStop: {
    light: 'theme.graph.gradient.stop._light',
    dark: 'theme.graph.gradient.stop._dark',
  },
  bar: {
    light: 'theme.graph.line._light',
    dark: 'theme.graph.line._dark',
  },
  axis: {
    light: 'text.secondary',
    dark: 'text.secondary',
  },
  grid: {
    light: 'border.divider',
    dark: 'border.divider',
  },
};

export function getChartColorToken(role: ChartColorRole, colorMode: ChartColorMode): string {
  return CHART_COLOR_TOKENS[role][colorMode];
}
