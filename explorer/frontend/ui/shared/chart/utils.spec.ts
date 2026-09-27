import { describe, expect, it } from 'vitest';

import { CHART_COLOR_TOKENS, formatDate, getChartColorToken, sortByDateDesc } from './utils';

describe('formatDate', () => {
  it('cuts a date down to the day the chart queries ask for', () => {
    expect(formatDate(new Date('2026-03-09T18:42:11.000Z'))).toBe('2026-03-09');
  });
});

describe('sortByDateDesc', () => {
  it('orders two chart items by their date', () => {
    const earlier = { date: new Date('2026-03-01T00:00:00.000Z') };
    const later = { date: new Date('2026-03-02T00:00:00.000Z') };

    expect(sortByDateDesc(earlier, later)).toBeLessThan(0);
    expect(sortByDateDesc(later, earlier)).toBeGreaterThan(0);
    expect(sortByDateDesc(earlier, earlier)).toBe(0);
  });
});

describe('CHART_COLOR_TOKENS', () => {
  it('names a theme token for the line, the area, the bar, the axis and the grid', () => {
    expect(Object.keys(CHART_COLOR_TOKENS).sort()).toEqual([ 'areaStart', 'areaStop', 'axis', 'bar', 'grid', 'line' ]);
  });

  it('carries both appearances for every chart colour', () => {
    Object.values(CHART_COLOR_TOKENS).forEach((token) => {
      expect(token.light.length).toBeGreaterThan(0);
      expect(token.dark.length).toBeGreaterThan(0);
    });
  });

  it('writes no literal colour: every entry is a token path', () => {
    Object.values(CHART_COLOR_TOKENS).forEach((token) => {
      [ token.light, token.dark ].forEach((value) => {
        expect(value).not.toMatch(/^#|^rgb|^hsl/);
        expect(value).toMatch(/^[a-z][\w.]*$/);
      });
    });
  });

  it('takes the line and the bar from the same graph token so the chart reads as one mark', () => {
    expect(CHART_COLOR_TOKENS.bar).toEqual(CHART_COLOR_TOKENS.line);
    expect(CHART_COLOR_TOKENS.line.light).toBe('theme.graph.line._light');
    expect(CHART_COLOR_TOKENS.line.dark).toBe('theme.graph.line._dark');
  });
});

describe('getChartColorToken', () => {
  it('answers the token of the appearance it is asked for', () => {
    expect(getChartColorToken('line', 'light')).toBe(CHART_COLOR_TOKENS.line.light);
    expect(getChartColorToken('line', 'dark')).toBe(CHART_COLOR_TOKENS.line.dark);
    expect(getChartColorToken('areaStart', 'light')).toBe('theme.graph.gradient.start._light');
    expect(getChartColorToken('areaStop', 'dark')).toBe('theme.graph.gradient.stop._dark');
  });

  it('answers one semantic token for the axis and the grid in either appearance', () => {
    expect(getChartColorToken('axis', 'light')).toBe(getChartColorToken('axis', 'dark'));
    expect(getChartColorToken('grid', 'light')).toBe(getChartColorToken('grid', 'dark'));
    expect(getChartColorToken('axis', 'light')).toBe('text.secondary');
    expect(getChartColorToken('grid', 'light')).toBe('border.divider');
  });
});
