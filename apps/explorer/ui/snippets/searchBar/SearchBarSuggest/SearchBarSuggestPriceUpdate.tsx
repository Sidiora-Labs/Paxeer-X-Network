import { chakra, Text, Flex } from '@chakra-ui/react';
import React from 'react';

import type { ItemsProps } from './types';
import type { SearchResultPriceUpdate } from 'types/api/search';

import { Badge } from 'toolkit/chakra/badge';
import * as TxEntity from 'ui/shared/entities/tx/TxEntity';
import HashStringShortenDynamic from 'ui/shared/HashStringShortenDynamic';
import Time from 'ui/shared/time/Time';

const SearchBarSuggestPriceUpdate = ({ data, isMobile }: ItemsProps<SearchResultPriceUpdate>) => {
  const icon = <TxEntity.Icon/>;
  const hash = (
    <chakra.mark overflow="hidden" whiteSpace="nowrap" fontWeight={ 700 }>
      <HashStringShortenDynamic hash={ data.hash } noTooltip/>
    </chakra.mark>
  );
  const badge = <Badge colorPalette="green" flexShrink={ 0 } ml={ 2 }>Price update</Badge>;
  const date = <Time timestamp={ data.timestamp } format="lll_s"/>;

  if (isMobile) {
    return (
      <>
        <Flex alignItems="center">
          { icon }
          { hash }
          { badge }
        </Flex>
        <Text color="text.secondary">{ date }</Text>
      </>
    );
  }

  return (
    <Flex columnGap={ 2 }>
      <Flex alignItems="center" minW={ 0 }>
        { icon }
        { hash }
        { badge }
      </Flex>
      <Text color="text.secondary" textAlign="end" flexShrink={ 0 } ml="auto">{ date }</Text>
    </Flex>
  );
};

export default React.memo(SearchBarSuggestPriceUpdate);
