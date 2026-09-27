import type { StackProps } from '@chakra-ui/react';
import { Box, VStack } from '@chakra-ui/react';
import React from 'react';

import type { HighlightsItemProps } from './highlights/HighlightsItem';
import HighlightsItem from './highlights/HighlightsItem';

export interface HighlightsProps extends StackProps {
  items?: Array<HighlightsItemProps>;
}

const Highlights = ({ items, ...rest }: HighlightsProps) => {
  if (!items || items.length === 0) {
    return null;
  }

  return (
    <VStack data-label="home-highlights" alignItems="stretch" justifyContent="center" gap={ 0 } h="100%" { ...rest }>
      { items.map((item, index) => (
        <Box
          key={ item.id }
          data-divided={ index > 0 ? true : undefined }
          borderTopWidth={ index > 0 ? '1px' : '0' }
          borderStyle="solid"
          borderColor="border.divider"
        >
          <HighlightsItem { ...item }/>
        </Box>
      )) }
    </VStack>
  );
};

export default React.memo(Highlights);
