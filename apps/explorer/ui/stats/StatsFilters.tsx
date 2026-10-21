import { createListCollection, Grid, GridItem } from '@chakra-ui/react';
import React from 'react';

import type * as stats from '@blockscout/stats-types';
import type { StatsIntervalIds } from 'types/client/stats';

import { Select } from 'toolkit/chakra/select';
import { FilterInput } from 'toolkit/components/filters/FilterInput';
import ChartIntervalSelect from 'ui/shared/chart/ChartIntervalSelect';

type Props = {
  sections?: Array<stats.LineChartSection>;
  currentSection: string;
  onSectionChange: (newSection: string) => void;
  interval: StatsIntervalIds;
  onIntervalChange: (newInterval: StatsIntervalIds) => void;
  onFilterInputChange: (q: string) => void;
  isLoading: boolean;
  initialFilterValue: string;
};

const StatsFilters = ({
  sections,
  currentSection,
  onSectionChange,
  interval,
  onIntervalChange,
  onFilterInputChange,
  isLoading,
  initialFilterValue,
}: Props) => {

  const collection = React.useMemo(() => {
    return createListCollection({
      items: [
        { value: 'all', label: 'All stats' },
        ...(sections || []).map((section) => ({ value: section.id, label: section.title })),
      ],
    });
  }, [ sections ]);

  const handleItemSelect = React.useCallback(({ value }: { value: Array<string> }) => {
    onSectionChange(value[0]);
  }, [ onSectionChange ]);

  return (
    <Grid
      data-stats-filters
      gap={{ base: 2, lg: 3 }}
      templateAreas={{
        base: `"section interval"
                "input input"`,
        lg: `"input section interval"`,
      }}
      gridTemplateColumns={{ base: 'repeat(2, minmax(0, 1fr))', lg: '1fr auto auto' }}
      alignItems="center"
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={{ base: 3, lg: 4 }}
      py={{ base: 3, lg: 3 }}
    >
      <GridItem
        w={{ base: '100%', lg: 'auto' }}
        area="section"
        data-stats-filter-section
      >
        <Select
          collection={ collection }
          placeholder="Select section"
          defaultValue={ [ currentSection ] }
          onValueChange={ handleItemSelect }
          w={{ base: '100%', lg: '160px' }}
          loading={ isLoading }
        />
      </GridItem>

      <GridItem
        w={{ base: '100%', lg: 'auto' }}
        area="interval"
        data-stats-filter-interval
      >
        <ChartIntervalSelect interval={ interval } onIntervalChange={ onIntervalChange } isLoading={ isLoading } selectTagSize="md"/>
      </GridItem>

      <GridItem
        w="100%"
        area="input"
        data-stats-filter-input
      >
        <FilterInput
          key={ initialFilterValue }
          loading={ isLoading }
          onChange={ onFilterInputChange }
          placeholder="Find chart, metric..."
          initialValue={ initialFilterValue }
          size="sm"
        />
      </GridItem>
    </Grid>
  );
};

export default StatsFilters;
