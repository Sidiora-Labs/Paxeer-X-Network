import { HStack, Box } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import RewardsButton from 'ui/rewards/RewardsButton';
import SearchBar from 'ui/snippets/searchBar/SearchBarDesktop';
import UserProfileDesktop from 'ui/snippets/user/UserProfileDesktop';

type Props = {
  renderSearchBar?: () => React.ReactNode;
};

const HeaderDesktop = ({ renderSearchBar }: Props) => {

  // with the horizontal navigation the search box sits in the utility bar, so the content column
  // only carries a search box when the page brings its own
  if (config.UI.navigation.layout === 'horizontal' && !renderSearchBar) {
    return null;
  }

  const searchBar = renderSearchBar ? renderSearchBar() : <SearchBar/>;

  return (
    <HStack
      as="header"
      data-label="content-search"
      display={{ base: 'none', lg: 'flex' }}
      width="100%"
      alignItems="center"
      justifyContent="center"
      gap={ 6 }
    >
      <Box width="100%">
        { searchBar }
      </Box>
      { config.UI.navigation.layout === 'vertical' && (
        <Box display="flex" gap={ 2 } flexShrink={ 0 }>
          { config.features.rewards.isEnabled && <RewardsButton/> }
          <UserProfileDesktop buttonVariant="header"/>
        </Box>
      ) }
    </HStack>
  );
};

export default React.memo(HeaderDesktop);
