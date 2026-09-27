import { Box, chakra, Grid } from '@chakra-ui/react';
import React from 'react';

import type { TokenInfo, TokenInstance } from 'types/api/token';

import config from 'configs/app';
import useIsMounted from 'lib/hooks/useIsMounted';
import { Skeleton } from 'toolkit/chakra/skeleton';
import AppActionButton from 'ui/shared/AppActionButton/AppActionButton';
import useAppActionData from 'ui/shared/AppActionButton/useAppActionData';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import DetailedInfoSponsoredItem from 'ui/shared/DetailedInfo/DetailedInfoSponsoredItem';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import HashStringShortenDynamic from 'ui/shared/HashStringShortenDynamic';
import NftMedia from 'ui/shared/nft/NftMedia';
import { ScanKeyValue } from 'ui/shared/scan';
import TokenNftMarketplaces from 'ui/token/TokenNftMarketplaces';

import TokenInstanceCreatorAddress from './details/TokenInstanceCreatorAddress';
import TokenInstanceMetadataInfo from './details/TokenInstanceMetadataInfo';
import TokenInstanceTransfersCount from './details/TokenInstanceTransfersCount';

interface CardProps {
  title: string;
  children: React.ReactNode;
  mt?: number;
}

const DetailsCard = ({ title, children, mt }: CardProps) => {
  return (
    <Box
      mt={ mt }
      data-token-card={ title }
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={ 4 }
      py={ 4 }
      minW={ 0 }
    >
      <chakra.h2 textStyle="sm" fontWeight="600" color="text.primary" mb={ 3 } data-card-title>{ title }</chakra.h2>
      <DetailedInfo.Container
        templateColumns="minmax(0, 1fr)"
        columnGap={ 0 }
        rowGap={ 2 }
        textStyle="sm"
      >
        { children }
      </DetailedInfo.Container>
    </Box>
  );
};

interface Props {
  data?: TokenInstance;
  token?: TokenInfo;
  isLoading?: boolean;
  scrollRef?: React.RefObject<HTMLDivElement | null>;
}

const TokenInstanceDetails = ({ data, token, scrollRef, isLoading }: Props) => {
  const appActionData = useAppActionData(token?.address_hash, !isLoading);
  const isMounted = useIsMounted();

  const handleCounterItemClick = React.useCallback(() => {
    window.setTimeout(() => {
      // cannot do scroll instantly, have to wait a little
      scrollRef?.current?.scrollIntoView({ behavior: 'smooth' });
    }, 500);
  }, [ scrollRef ]);

  if (!data || !token || !isMounted) {
    return null;
  }

  return (
    <Box data-token-instance-details mb={ 6 }>
      <Grid
        templateColumns={{ base: 'minmax(0, 1fr)', lg: 'repeat(3, minmax(0, 1fr))' }}
        gap={ 4 }
        alignItems="start"
      >
        <DetailsCard title="Media">
          <NftMedia
            data={ data }
            isLoading={ isLoading }
            size="md"
            withFullscreen
            w="100%"
            maxW="250px"
            alignSelf="center"
          />
        </DetailsCard>

        <DetailsCard title="Overview">
          { data.is_unique && data.owner && (
            <ScanKeyValue
              label="Owner"
              hint="Current owner of this token instance"
              isLoading={ isLoading }
              multiRow
            >
              <AddressEntity
                address={ data.owner }
                isLoading={ isLoading }
              />
            </ScanKeyValue>
          ) }

          <TokenInstanceCreatorAddress hash={ isLoading ? '' : token.address_hash }/>

          <ScanKeyValue
            label="Token ID"
            hint="This token instance unique token ID"
            isLoading={ isLoading }
            multiRow
          >
            <Box display="flex" alignItems="center" overflow="hidden" data-token-instance-id>
              <Skeleton loading={ isLoading } overflow="hidden" display="inline-block" w="100%">
                <HashStringShortenDynamic hash={ data.id }/>
              </Skeleton>
              <CopyToClipboard text={ data.id } isLoading={ isLoading }/>
            </Box>
          </ScanKeyValue>

          <TokenInstanceTransfersCount
            hash={ isLoading ? '' : token.address_hash }
            id={ isLoading ? '' : data.id }
            onClick={ handleCounterItemClick }
          />
        </DetailsCard>

        <DetailsCard title="Other info">
          <TokenNftMarketplaces
            isLoading={ isLoading }
            hash={ token.address_hash }
            id={ data.id }
            appActionData={ appActionData }
            source="NFT item"
          />

          { (config.UI.views.nft.marketplaces.length === 0 && appActionData) && (
            <ScanKeyValue label="Dapp" hint="Link to the dapp">
              <AppActionButton data={ appActionData } height="30px" source="NFT item"/>
            </ScanKeyValue>
          ) }

          <DetailedInfoSponsoredItem isLoading={ isLoading }/>
        </DetailsCard>
      </Grid>

      <DetailsCard title="Metadata" mt={ 4 }>
        <TokenInstanceMetadataInfo data={ data } isLoading={ isLoading }/>
      </DetailsCard>
    </Box>
  );
};

export default React.memo(TokenInstanceDetails);
