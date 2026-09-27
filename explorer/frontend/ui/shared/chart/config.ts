import { useToken } from '@chakra-ui/react';
import React from 'react';

import type { ChartConfig } from 'toolkit/components/charts/types';

import useIsMobile from 'lib/hooks/useIsMobile';
import { useColorModeValue } from 'toolkit/chakra/color-mode';

import type { ChartColorRole } from './utils';
import { getChartColorToken } from './utils';

function useChartColor(role: ChartColorRole) {
  const token = useColorModeValue(getChartColorToken(role, 'light'), getChartColorToken(role, 'dark'));
  const [ color ] = useToken('colors', token);
  return color;
}

export function useChartsConfig(): Array<ChartConfig> {
  const lineColor = useDefaultLineColor();
  const gradient = useDefaultGradient();
  const isMobile = useIsMobile();

  return React.useMemo(() => [
    {
      type: 'line',
      color: lineColor,
      strokeWidth: isMobile ? 1 : 2,
    },
    {
      type: 'area',
      gradient,
    },
  ], [ lineColor, isMobile, gradient ]);
}

export function useDefaultLineColor() {
  const lineColor = useChartColor('line');
  return React.useMemo(() => lineColor, [ lineColor ]);
}

export function useDefaultGradient() {
  const startColor = useChartColor('areaStart');
  const stopColor = useChartColor('areaStop');
  return React.useMemo(() => ({ startColor, stopColor }), [ startColor, stopColor ]);
}

export function useDefaultBarColor() {
  const barColor = useChartColor('bar');
  return React.useMemo(() => barColor, [ barColor ]);
}

export function useChartAxisColors() {
  const axisColor = useChartColor('axis');
  const gridColor = useChartColor('grid');
  return React.useMemo(() => ({ axisColor, gridColor }), [ axisColor, gridColor ]);
}
