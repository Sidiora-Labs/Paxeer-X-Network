import React from 'react';

import type { TimeChartData } from 'toolkit/components/charts/types';

import { ChartArea, ChartAxis, ChartLine, ChartOverlay, ChartTooltip, useTimeChartController } from 'toolkit/components/charts';
import { useDefaultGradient, useDefaultLineColor } from 'ui/shared/chart/config';

interface Props {
  data: TimeChartData;
  caption?: string;
}

const CHART_MARGIN = { bottom: 20, left: 0, right: 10, top: 5 };
const X_AXIS_TICKS = 3;
const Y_AXIS_TICKS = 2;

const ChainIndicatorChartContent = ({ data }: Props) => {
  const overlayRef = React.useRef<SVGRectElement>(null);
  const lineColor = useDefaultLineColor();
  const gradient = useDefaultGradient();

  const axesConfig = React.useMemo(() => {
    return {
      x: { ticks: X_AXIS_TICKS },
      y: { ticks: Y_AXIS_TICKS, nice: true },
    };
  }, [ ]);

  const { rect, ref, axes, innerWidth, innerHeight, chartMargin } = useTimeChartController({
    data,
    margin: CHART_MARGIN,
    axesConfig,
  });

  return (
    <svg width="100%" height="100%" ref={ ref } cursor="pointer" data-label="sparkline">
      <g transform={ `translate(${ chartMargin.left || 0 },${ chartMargin.top || 0 })` } opacity={ rect ? 1 : 0 }>
        <ChartArea
          id={ data[0].id }
          data={ data[0].items }
          xScale={ axes.x.scale }
          yScale={ axes.y.scale }
          gradient={ gradient }
        />
        <ChartLine
          data={ data[0].items }
          xScale={ axes.x.scale }
          yScale={ axes.y.scale }
          stroke={ lineColor }
          strokeWidth={ 3 }
          animation="left"
        />
        <ChartAxis
          data-label="sparkline-y-axis"
          type="left"
          scale={ axes.y.scale }
          ticks={ Y_AXIS_TICKS }
          tickFormatGenerator={ axes.y.tickFormatter }
          noAnimation
        />
        <ChartAxis
          data-label="sparkline-x-axis"
          type="bottom"
          scale={ axes.x.scale }
          transform={ `translate(0, ${ innerHeight })` }
          ticks={ X_AXIS_TICKS }
          tickFormatGenerator={ axes.x.tickFormatter }
          noAnimation
        />
        <ChartOverlay ref={ overlayRef } width={ innerWidth } height={ innerHeight }>
          <ChartTooltip
            anchorEl={ overlayRef.current }
            width={ innerWidth }
            height={ innerHeight }
            xScale={ axes.x.scale }
            yScale={ axes.y.scale }
            data={ data }
          />
        </ChartOverlay>
      </g>
    </svg>
  );
};

export default React.memo(ChainIndicatorChartContent);
