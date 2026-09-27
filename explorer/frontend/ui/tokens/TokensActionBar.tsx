import { createListCollection, HStack } from '@chakra-ui/react';
import React from 'react';

import type { TokensSortingValue } from 'types/api/tokens';

import { FilterInput } from 'toolkit/components/filters/FilterInput';
import Sort from 'ui/shared/sort/Sort';
import { SORT_OPTIONS } from 'ui/tokens/utils';

const sortCollection = createListCollection({
  items: SORT_OPTIONS,
});

interface Props {
  searchTerm: string | undefined;
  onSearchChange: (value: string) => void;
  sort: TokensSortingValue;
  onSortChange: (value: TokensSortingValue) => void;
  filter: React.ReactNode;
}

const TokensActionBar = ({
  sort,
  onSortChange,
  searchTerm,
  onSearchChange,
  filter,
}: Props) => {

  const handleSortChange = React.useCallback(({ value }: { value: Array<string> }) => {
    onSortChange(value[0] as TokensSortingValue);
  }, [ onSortChange ]);

  return (
    <HStack gap={ 2 } flexWrap="wrap" data-tokens-controls>
      { filter }
      <Sort
        name="tokens_sorting"
        defaultValue={ [ sort ] }
        collection={ sortCollection }
        onValueChange={ handleSortChange }
        hideFrom="lg"
      />
      <FilterInput
        w={{ base: '100%', lg: '260px' }}
        size="sm"
        onChange={ onSearchChange }
        placeholder="Token name or symbol"
        initialValue={ searchTerm }
      />
    </HStack>
  );
};

export default React.memo(TokensActionBar);
