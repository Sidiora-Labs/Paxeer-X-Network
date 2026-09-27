import { Box, Flex } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import { Button } from 'toolkit/chakra/button';
import { Heading } from 'toolkit/chakra/heading';
import RewardsButton from 'ui/rewards/RewardsButton';
import IconSvg from 'ui/shared/IconSvg';
import SearchBar from 'ui/snippets/searchBar/SearchBarDesktop';
import SearchBarMobile from 'ui/snippets/searchBar/SearchBarMobile';
import UserProfileDesktop from 'ui/snippets/user/UserProfileDesktop';

export const BACKGROUND_DEFAULT =
  'radial-gradient(103.03% 103.03% at 0% 0%, rgba(183, 148, 244, 0.8) 0%, rgba(0, 163, 196, 0.8) 100%), var(--chakra-colors-blue-400)';

const HeroBanner = () => {
  const searchRef = React.useRef<HTMLDivElement>(null);

  const title = config.meta.seo.enhancedDataEnabled ?
    `${ config.chain.name } blockchain explorer` :
    `${ config.chain.name } explorer`;

  const handleSubmitClick = React.useCallback(() => {
    searchRef.current?.querySelector('form')?.requestSubmit();
  }, []);

  return (
    <Box as="section" data-label="hero" w="100%" pb={{ base: 4, lg: 8 }}>
      <Flex mb={{ base: 3, lg: 5 }} justifyContent="space-between" alignItems="center" columnGap={ 2 }>
        <Heading level="1" data-label="hero-title">{ title }</Heading>
        { config.UI.navigation.layout === 'vertical' && (
          <Box display={{ base: 'none', lg: 'flex' }} gap={ 2 }>
            { config.features.rewards.isEnabled && <RewardsButton variant="hero"/> }
            <UserProfileDesktop buttonVariant="hero"/>
          </Box>
        ) }
      </Flex>
      <Flex data-label="hero-search" alignItems="center" columnGap={ 2 } w="100%">
        <Box display={{ base: 'flex', lg: 'none' }} flexGrow={ 1 } minW={ 0 }>
          <SearchBarMobile isHeroBanner/>
        </Box>
        <Box ref={ searchRef } data-label="hero-search-desktop" display={{ base: 'none', lg: 'flex' }} flexGrow={ 1 } minW={ 0 }>
          <SearchBar isHeroBanner/>
        </Box>
        <Button
          data-label="hero-search-submit"
          display={{ base: 'none', lg: 'inline-flex' }}
          variant="solid"
          size="md"
          aria-label="Search"
          onClick={ handleSubmitClick }
        >
          <IconSvg name="search" boxSize={ 5 }/>
        </Button>
      </Flex>
    </Box>
  );
};

export default React.memo(HeroBanner);
