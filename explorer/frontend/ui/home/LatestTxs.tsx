import { Box, Flex } from '@chakra-ui/react';
import React from 'react';

import { route } from 'nextjs-routes';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import { AddressHighlightProvider } from 'lib/contexts/addressHighlight';
import useIsMobile from 'lib/hooks/useIsMobile';
import { TX } from 'stubs/tx';
import { Link } from 'toolkit/chakra/link';
import IconSvg from 'ui/shared/IconSvg';
import SocketNewItemsNotice from 'ui/shared/SocketNewItemsNotice';
import useNewTxsSocket from 'ui/txs/socket/useTxsSocketTypeAll';

import LatestTxsDegraded from './fallbacks/LatestTxsDegraded';
import LatestTxsItem from './LatestTxsItem';
import LatestTxsItemMobile from './LatestTxsItemMobile';

const zetachainFeature = config.features.zetachain;

const LatestTxs = () => {
  const isMobile = useIsMobile();
  const txsCount = isMobile ? 4 : 6;
  const { data, isPlaceholderData, isError } = useApiQuery('general:homepage_txs', {
    queryOptions: {
      placeholderData: Array(txsCount).fill(TX),
    },
  });

  const { num, showErrorAlert } = useNewTxsSocket({ type: 'txs_home', isLoading: isPlaceholderData });

  if (isError) {
    return <Box px={{ base: 3, lg: 4 }} py={ 3 }><LatestTxsDegraded maxNum={ txsCount }/></Box>;
  }

  if (data) {
    const txsUrl = route({ pathname: `/txs`, query: zetachainFeature.isEnabled ? { tab: 'evm' } : undefined });
    return (
      <>
        <SocketNewItemsNotice borderRadius={ 0 } url={ txsUrl } num={ num } showErrorAlert={ showErrorAlert } isLoading={ isPlaceholderData }/>
        <Box display={{ base: 'block', lg: 'none' }}>
          { data.slice(0, txsCount).map(((tx, index) => (
            <LatestTxsItemMobile
              key={ tx.hash + (isPlaceholderData ? index : '') }
              tx={ tx }
              isLoading={ isPlaceholderData }
            />
          ))) }
        </Box>
        <AddressHighlightProvider>
          <Box display={{ base: 'none', lg: 'block' }} minW="720px">
            { data.slice(0, txsCount).map(((tx, index) => (
              <LatestTxsItem
                key={ tx.hash + (isPlaceholderData ? index : '') }
                tx={ tx }
                isLoading={ isPlaceholderData }
              />
            ))) }
          </Box>
        </AddressHighlightProvider>
        <Flex data-label="view-all-txs" justifyContent="center" px={ 4 } py={ 3 } borderTopWidth="1px" borderStyle="solid" borderColor="border.divider">
          <Link
            textStyle="xs"
            fontWeight="600"
            textTransform="uppercase"
            letterSpacing="wide"
            loading={ isPlaceholderData }
            href={ txsUrl }
          >
            View all transactions<IconSvg name="arrows/east-mini" boxSize={ 4 } ml={ 1 }/>
          </Link>
        </Flex>
      </>
    );
  }

  return <Box px={{ base: 3, lg: 4 }} py={ 3 } textStyle="sm">No latest transactions found.</Box>;
};

export default LatestTxs;
