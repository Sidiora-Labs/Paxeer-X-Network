import { Box, Grid, GridItem } from '@chakra-ui/react';
import React from 'react';

import config from 'configs/app';
import useIsMobile from 'lib/hooks/useIsMobile';
import { HomeRpcDataContextProvider } from 'ui/home/fallbacks/rpcDataContext';
import HeroBanner from 'ui/home/HeroBanner';
import LatestArbitrumL2Batches from 'ui/home/latestBatches/LatestArbitrumL2Batches';
import LatestZkEvmL2Batches from 'ui/home/latestBatches/LatestZkEvmL2Batches';
import LatestBlocks from 'ui/home/LatestBlocks';
import Stats from 'ui/home/Stats';
import Transactions from 'ui/home/Transactions';
import AdBanner from 'ui/shared/ad/AdBanner';

const rollupFeature = config.features.rollup;

const Home = () => {
  const isMobile = useIsMobile();

  const leftWidget = (() => {
    if (rollupFeature.isEnabled && !rollupFeature.homepage.showLatestBlocks) {
      switch (rollupFeature.type) {
        case 'zkEvm':
          return <LatestZkEvmL2Batches/>;
        case 'arbitrum':
          return <LatestArbitrumL2Batches/>;
      }
    }

    return <LatestBlocks/>;
  })();

  return (
    <HomeRpcDataContextProvider>
      <Box as="main">
        <HeroBanner/>
        <Stats/>
        { isMobile && <AdBanner mt={ 4 } mx="auto" justifyContent="center" format="mobile"/> }
        <Grid
          data-label="home-lists"
          mt={{ base: 4, lg: 6 }}
          templateColumns={{ base: '1fr', lg: 'repeat(2, minmax(0, 1fr))' }}
          columnGap={{ base: 0, lg: 6 }}
          rowGap={{ base: 4, lg: 0 }}
          alignItems="start"
        >
          <GridItem minW={ 0 }>
            { leftWidget }
          </GridItem>
          <GridItem minW={ 0 }>
            <Transactions/>
          </GridItem>
        </Grid>
      </Box>
    </HomeRpcDataContextProvider>
  );
};

export default Home;
