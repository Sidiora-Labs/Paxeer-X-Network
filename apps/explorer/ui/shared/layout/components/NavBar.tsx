import { Box } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import NavigationDesktop from 'ui/snippets/navigation/horizontal/NavigationDesktop';

const STICKY_HEADER_CSS = {
  '& [data-label="brand-row"]': {
    bgColor: 'header.sticky.bg',
    backdropFilter: 'blur(12px)',
  },
};

const StickyNavigation = (): React.JSX.Element => {
  return (
    <Box
      data-label="sticky-header"
      position={{ base: 'static', lg: 'sticky' }}
      top={ 0 }
      zIndex="sticky"
      css={ STICKY_HEADER_CSS }
    >
      <NavigationDesktop/>
    </Box>
  );
};

const EmptyComponent = (): null => null;

export default config.UI.navigation.layout === 'horizontal' ? StickyNavigation : EmptyComponent;
