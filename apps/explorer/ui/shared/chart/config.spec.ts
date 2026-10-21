// @vitest-environment jsdom

import { useToken } from '@chakra-ui/react';

import { useColorModeValue } from 'toolkit/chakra/color-mode';
import { Wrapper } from 'ui/shared/layout/testWrapper';
import { describe, expect, it } from 'vitest';
import { renderHook } from 'vitest/lib';

import { useChartAxisColors, useChartsConfig, useDefaultBarColor, useDefaultGradient, useDefaultLineColor } from './config';
import { CHART_COLOR_TOKENS } from './utils';

// The hooks are read beside the tokens they are supposed to come from, in one render, so a literal
// colour written into the config would not match what the theme answers for the same token.
const useChartColorsUnderTest = () => {
  const resolve = (role: keyof typeof CHART_COLOR_TOKENS) => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const token = useColorModeValue(CHART_COLOR_TOKENS[role].light, CHART_COLOR_TOKENS[role].dark);
    // eslint-disable-next-line react-hooks/rules-of-hooks
    return useToken('colors', token)[0];
  };

  return {
    chartsConfig: useChartsConfig(),
    lineColor: useDefaultLineColor(),
    gradient: useDefaultGradient(),
    barColor: useDefaultBarColor(),
    axisColors: useChartAxisColors(),
    expected: {
      line: resolve('line'),
      areaStart: resolve('areaStart'),
      areaStop: resolve('areaStop'),
      bar: resolve('bar'),
      axis: resolve('axis'),
      grid: resolve('grid'),
    },
  };
};

const renderColors = () => renderHook(useChartColorsUnderTest, { wrapper: Wrapper }).result.current;

describe('the chart colour configuration', () => {
  it('draws a line over an area, in that order', () => {
    const { chartsConfig } = renderColors();

    expect(chartsConfig.map((entry) => entry.type)).toEqual([ 'line', 'area' ]);
  });

  it('takes the line colour from the graph line token', () => {
    const { chartsConfig, lineColor, expected } = renderColors();
    const [ line ] = chartsConfig;

    expect(expected.line.length).toBeGreaterThan(0);
    expect(lineColor).toBe(expected.line);
    expect(line.type === 'line' && line.color).toBe(expected.line);
  });

  it('takes the area gradient from the graph gradient tokens', () => {
    const { chartsConfig, gradient, expected } = renderColors();
    const area = chartsConfig[1];

    expect(gradient).toEqual({ startColor: expected.areaStart, stopColor: expected.areaStop });
    expect(area.type === 'area' && area.gradient).toEqual({ startColor: expected.areaStart, stopColor: expected.areaStop });
  });

  it('takes the bar colour from a theme token as well', () => {
    const { barColor, expected } = renderColors();

    expect(expected.bar.length).toBeGreaterThan(0);
    expect(barColor).toBe(expected.bar);
  });

  it('takes the axis and the grid colours from theme tokens', () => {
    const { axisColors, expected } = renderColors();

    expect(expected.axis.length).toBeGreaterThan(0);
    expect(expected.grid.length).toBeGreaterThan(0);
    expect(axisColors).toEqual({ axisColor: expected.axis, gridColor: expected.grid });
  });
});
