import { chakra, Flex } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import useApiQuery from 'lib/api/useApiQuery';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import throwOnResourceLoadError from 'lib/errors/throwOnResourceLoadError';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Skeleton } from 'toolkit/chakra/skeleton';
import PaxeerXFeedUpdateDetails from 'ui/paxeerX/feed/PaxeerXFeedUpdateDetails';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import PageTitle from 'ui/shared/Page/PageTitle';

const PaxeerXFeedUpdate = () => {
  const router = useRouter();
  const hash = getQueryParamString(router.query.hash);
  throwOnAbsentParamError(hash);

  const updateQuery = useApiQuery('general:paxeer_x_feed_update', { pathParams: { hash } });

  if (updateQuery.isError) {
    throwOnResourceLoadError(updateQuery);
  }

  const isLoading = updateQuery.isPending;

  const titleAfter = (
    <Flex alignItems="center" columnGap={ 2 } ml={{ base: 0, lg: 3 }} minW={ 0 }>
      <Skeleton loading={ isLoading } overflow="hidden" minW={ 0 }>
        <chakra.span textStyle="lg" color="text.secondary" wordBreak="break-all">{ hash }</chakra.span>
      </Skeleton>
      <CopyToClipboard text={ hash } isLoading={ isLoading }/>
    </Flex>
  );

  return (
    <>
      <PageTitle title="Price update" afterTitle={ titleAfter } isLoading={ isLoading } withTextAd/>
      { updateQuery.data ? <PaxeerXFeedUpdateDetails data={ updateQuery.data }/> : null }
    </>
  );
};

export default PaxeerXFeedUpdate;
