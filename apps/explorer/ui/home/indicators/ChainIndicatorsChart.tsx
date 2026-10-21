import { chakra, Flex, Text } from '@chakra-ui/react';
import React from 'react';

import { Skeleton } from 'toolkit/chakra/skeleton';
import { Hint } from 'toolkit/components/Hint/Hint';
import { mdash } from 'toolkit/utils/htmlEntities';
import FallbackChart from 'ui/shared/fallbacks/FallbackChart';
import IconSvg from 'ui/shared/IconSvg';

import ChainIndicatorChartContainer from './ChainIndicatorChartContainer';
import type { UseFetchChartDataResult } from './useChartDataQuery';

interface Props {
  isLoading: boolean;
  value?: string;
  valueDiff?: number;
  chartQuery: UseFetchChartDataResult;
  title: string;
  hint?: string;
  days?: number;
}

const ChainIndicatorsChart = ({ isLoading: isLoadingProp, value, valueDiff, chartQuery, title, hint, days }: Props) => {
  const isLoading = isLoadingProp || chartQuery.isPending;

  const data = React.useMemo(() => {
    if (!days) {
      return chartQuery.data;
    }

    return chartQuery.data.map((item) => ({ ...item, items: item.items.slice(-days) }));
  }, [ chartQuery.data, days ]);

  const valueTitleElement = (() => {
    if (value === undefined) {
      return null;
    }

    if (isLoading) {
      return <Skeleton loading h="28px" w="160px"/>;
    }

    if (value.includes('N/A')) {
      return <Text textStyle="heading.sm" opacity="control.disabled">{ mdash }</Text>;
    }

    return <Text textStyle="heading.sm">{ value }</Text>;
  })();

  const valueDiffElement = (() => {
    if (valueDiff === undefined || (!isLoading && value?.includes('N/A'))) {
      return null;
    }

    const diffColor = valueDiff >= 0 ? 'stat.indicator.up' : 'stat.indicator.down';

    return (
      <Skeleton loading={ isLoading } display="flex" alignItems="center" color={ diffColor } ml={ 2 }>
        <IconSvg name="arrows/up-head" boxSize={ 5 } mr={ 1 } transform={ valueDiff < 0 ? 'rotate(180deg)' : 'rotate(0)' }/>
        <Text color={ diffColor } fontWeight="600">{ valueDiff }%</Text>
      </Skeleton>
    );
  })();

  if (chartQuery.isError) {
    return <FallbackChart term={ title } h={{ base: '144px', lg: '184px' }}/>;
  }

  return (
    <Flex flexGrow={ 1 } flexDir="column" h="100%" data-label="chain-indicator-chart" data-points={ data[0]?.items.length ?? 0 }>
      <Skeleton loading={ isLoading } display="flex" alignItems="center" w="fit-content" columnGap={ 1 }>
        <chakra.span
          data-title
          textStyle="xs"
          fontWeight="600"
          letterSpacing="wide"
          textTransform="uppercase"
          color="text.muted"
        >
          { title }
        </chakra.span>
        { hint && <Hint label={ hint } boxSize={ 4 }/> }
      </Skeleton>
      { valueTitleElement && (
        <Flex mt={ 1 } alignItems="flex-end">
          { valueTitleElement }
          { valueDiffElement }
        </Flex>
      ) }
      <Flex mt={ 2 } h={{ base: '96px', lg: '110px' }} alignItems="flex-start" flexGrow={ 1 }>
        <ChainIndicatorChartContainer data={ data } isError={ chartQuery.isError } isPending={ isLoading }/>
      </Flex>
    </Flex>
  );
};

export default React.memo(ChainIndicatorsChart);
