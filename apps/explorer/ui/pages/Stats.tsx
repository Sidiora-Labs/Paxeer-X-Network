import { Box, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import useEtherscanRedirects from 'lib/router/useEtherscanRedirects';
import PageTitle from 'ui/shared/Page/PageTitle';

import ChartsWidgetsList from '../stats/ChartsWidgetsList';
import { getStatsSectionAnchorId, getStatsSectionKeys, STATS_OVERVIEW_SECTION } from '../stats/constants';
import NumberWidgetsList from '../stats/NumberWidgetsList';
import StatsFilters from '../stats/StatsFilters';
import useStats from '../stats/useStats';

const Stats = () => {
  const {
    isPlaceholderData,
    isError,
    sections,
    currentSection,
    handleSectionChange,
    interval,
    handleIntervalChange,
    handleFilterChange,
    displayedCharts,
    initialFilterQuery,
  } = useStats();

  useEtherscanRedirects();

  const navItems = React.useMemo(() => getStatsSectionKeys(displayedCharts), [ displayedCharts ]);
  const [ sectionInView, setSectionInView ] = React.useState(STATS_OVERVIEW_SECTION.id);

  // The navigation follows the page: the topmost section crossing the reading band is the one it
  // marks, so a section scrolled to from the navigation stays marked once it settles.
  React.useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') {
      return;
    }

    const anchors = navItems
      .map(({ id }) => window.document.getElementById(getStatsSectionAnchorId(id)))
      .filter((element): element is HTMLElement => element !== null);

    if (anchors.length === 0) {
      return;
    }

    const observer = new IntersectionObserver((entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];

      if (visible) {
        setSectionInView(visible.target.id);
      }
    }, { rootMargin: '-96px 0px -55% 0px' });

    anchors.forEach((anchor) => observer.observe(anchor));

    return () => observer.disconnect();
  }, [ navItems ]);

  const handleNavItemClick = React.useCallback((event: React.MouseEvent<HTMLButtonElement>) => {
    const sectionId = event.currentTarget.getAttribute('data-section-id');

    if (!sectionId) {
      return;
    }

    setSectionInView(sectionId);
    window.document.getElementById(getStatsSectionAnchorId(sectionId))?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  return (
    <>
      <PageTitle
        title={ config.meta.seo.enhancedDataEnabled ? `${ config.chain.name } statistic & data` : `${ config.chain.name } stats` }
      />

      <Box mb={{ base: 4, lg: 6 }}>
        <StatsFilters
          isLoading={ isPlaceholderData }
          initialFilterValue={ initialFilterQuery }
          sections={ sections }
          currentSection={ currentSection }
          onSectionChange={ handleSectionChange }
          interval={ interval }
          onIntervalChange={ handleIntervalChange }
          onFilterInputChange={ handleFilterChange }
        />
      </Box>

      <Flex flexDirection={{ base: 'column', lg: 'row' }} alignItems="flex-start" columnGap={ 6 } w="100%" minW={ 0 }>
        <chakra.nav
          data-stats-section-nav
          position={{ base: 'static', lg: 'sticky' }}
          top={{ lg: 6 }}
          alignSelf="flex-start"
          flexShrink={ 0 }
          w={{ base: '100%', lg: '200px' }}
          maxW="100%"
          mb={{ base: 4, lg: 0 }}
          overflowX={{ base: 'auto', lg: 'visible' }}
        >
          <Flex
            as="ul"
            listStyleType="none"
            flexDirection={{ base: 'row', lg: 'column' }}
            columnGap={ 2 }
            rowGap={ 1 }
            minW={ 0 }
          >
            { navItems.map((item) => (
              <chakra.li key={ item.id } flexShrink={ 0 } w={{ base: 'auto', lg: '100%' }}>
                <chakra.button
                  type="button"
                  data-section-id={ item.id }
                  data-section-active={ item.id === sectionInView || undefined }
                  onClick={ handleNavItemClick }
                  w="100%"
                  textAlign="left"
                  whiteSpace="nowrap"
                  textStyle="sm"
                  fontWeight="500"
                  borderRadius="md"
                  px={ 3 }
                  py={ 2 }
                  color="link.navigation.fg"
                  _hover={{ color: 'link.navigation.fg.hover' }}
                  css={{
                    '&[data-section-active]': {
                      backgroundColor: 'link.navigation.bg.selected',
                      color: 'link.navigation.fg.selected',
                    },
                  }}
                >
                  { item.title }
                </chakra.button>
              </chakra.li>
            )) }
          </Flex>
        </chakra.nav>

        <Box flexGrow={ 1 } minW={ 0 } w="100%">
          <Box data-stats-section={ STATS_OVERVIEW_SECTION.id } mb={{ base: 8, lg: 10 }}>
            <chakra.h2
              id={ getStatsSectionAnchorId(STATS_OVERVIEW_SECTION.id) }
              scrollMarginTop={{ base: 20, lg: 24 }}
              mb={{ base: 3, lg: 4 }}
              textStyle="heading.sm"
              fontWeight="600"
              color="text.primary"
            >
              <chakra.span data-section-title>{ STATS_OVERVIEW_SECTION.title }</chakra.span>
            </chakra.h2>
            <NumberWidgetsList/>
          </Box>

          <ChartsWidgetsList
            initialFilterQuery={ initialFilterQuery }
            isError={ isError }
            isPlaceholderData={ isPlaceholderData }
            charts={ displayedCharts }
            interval={ interval }
            sections={ sections }
            selectedSectionId={ currentSection }
          />
        </Box>
      </Flex>
    </>
  );
};

export default Stats;
