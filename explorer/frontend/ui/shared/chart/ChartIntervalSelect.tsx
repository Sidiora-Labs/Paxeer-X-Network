import { chakra, createListCollection } from '@chakra-ui/react';
import React from 'react';

import type { StatsInterval, StatsIntervalIds } from 'types/client/stats';

import { Select } from 'toolkit/chakra/select';
import { Skeleton } from 'toolkit/chakra/skeleton';
import type { TagProps } from 'toolkit/chakra/tag';
import TagGroupSelect from 'ui/shared/tagGroupSelect/TagGroupSelect';
import { STATS_INTERVALS } from 'ui/stats/constants';

const intervalCollection = createListCollection({
  items: Object.keys(STATS_INTERVALS).map((id: string) => ({
    value: id,
    label: STATS_INTERVALS[id as StatsIntervalIds].title,
  })),
});

const intervalListShort = Object.keys(STATS_INTERVALS).map((id: string) => ({
  id: id,
  title: STATS_INTERVALS[id as StatsIntervalIds].shortTitle,
})) as Array<StatsInterval>;

type Props = {
  interval: StatsIntervalIds;
  onIntervalChange: (newInterval: StatsIntervalIds) => void;
  isLoading?: boolean;
  selectTagSize?: TagProps['size'];
  className?: string;
};

const ChartIntervalSelect = ({ interval, onIntervalChange, isLoading, selectTagSize, className }: Props) => {

  const handleItemSelect = React.useCallback(({ value }: { value: Array<string> }) => {
    onIntervalChange(value[0] as StatsIntervalIds);
  }, [ onIntervalChange ]);

  return (
    <chakra.div className={ className } data-chart-interval-select={ interval } w={{ base: '100%', lg: 'auto' }}>
      <Skeleton hideBelow="lg" borderRadius="base" loading={ isLoading }>
        <TagGroupSelect<StatsIntervalIds>
          items={ intervalListShort }
          onChange={ onIntervalChange }
          value={ interval }
          tagSize={ selectTagSize }
          gap={ 1 }
          bg="bg.surface"
          borderWidth="1px"
          borderStyle="solid"
          borderColor="border.divider"
          borderRadius="md"
          px={ 1 }
          py={ 1 }
        />
      </Skeleton>
      <Select
        collection={ intervalCollection }
        placeholder="Select interval"
        defaultValue={ [ interval ] }
        onValueChange={ handleItemSelect }
        hideFrom="lg"
        w="100%"
        loading={ isLoading }
      />
    </chakra.div>
  );
};

export default React.memo(ChartIntervalSelect);
