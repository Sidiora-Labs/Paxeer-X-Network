import { chakra, Flex } from '@chakra-ui/react';
import { pickBy } from 'es-toolkit';
import { useRouter } from 'next/router';
import React from 'react';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import throwOnResourceLoadError from 'lib/errors/throwOnResourceLoadError';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import PaxeerXReceiptDetails from 'ui/paxeerX/receipts/PaxeerXReceiptDetails';
import TextAd from 'ui/shared/ad/TextAd';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import IconSvg from 'ui/shared/IconSvg';
import PageTitle from 'ui/shared/Page/PageTitle';
import { ScanSectionTabs } from 'ui/shared/scan';

const apiDocsFeature = config.features.apiDocs;

const TABS = [ { id: 'index', title: 'Overview' } ];

const PaxeerXReceiptPageContent = () => {
  const router = useRouter();
  const id = getQueryParamString(router.query.id);

  const receiptQuery = useApiQuery('general:paxeer_x_receipt', {
    pathParams: { id },
    queryOptions: {
      enabled: Boolean(id),
    },
  });

  const handleTabChange = React.useCallback((value: string) => {
    const queryForPathname = pickBy(router.query, (_, key) => router.pathname.includes(`[${ String(key) }]`));

    router.push(
      { pathname: router.pathname, query: { ...queryForPathname, tab: value } },
      undefined,
      { shallow: true },
    );
  }, [ router ]);

  throwOnAbsentParamError(id);
  throwOnResourceLoadError(receiptQuery);

  const isLoading = receiptQuery.isPending;
  const data = receiptQuery.data;

  const apiEntry = apiDocsFeature.isEnabled ? (
    <Link href={ route({ pathname: '/api-docs' }) } textStyle="sm" display="inline-flex" alignItems="center" data-api-entry>
      <IconSvg name="API" boxSize={ 4 } mr={ 1 }/>
      API
    </Link>
  ) : null;

  const titleAfter = (
    <Flex alignItems="center" columnGap={ 2 } rowGap={ 2 } flexWrap="wrap" ml={{ base: 0, lg: 3 }} minW={ 0 } data-receipt-identifier>
      <Skeleton loading={ isLoading } overflow="hidden" minW={ 0 }>
        <chakra.span textStyle="lg" color="text.secondary" wordBreak="break-all">{ id }</chakra.span>
      </Skeleton>
      <CopyToClipboard text={ id } isLoading={ isLoading }/>
    </Flex>
  );

  return (
    <>
      <TextAd mb={ 6 }/>
      <PageTitle
        title="Kernel receipt"
        afterTitle={ titleAfter }
        isLoading={ isLoading }
      />
      <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }}>
        <ScanSectionTabs
          items={ TABS }
          value="index"
          onValueChange={ handleTabChange }
          rightSlot={ apiEntry }
        />
        { data ? <PaxeerXReceiptDetails data={ data } isLoading={ isLoading }/> : null }
      </Flex>
    </>
  );
};

export default PaxeerXReceiptPageContent;
