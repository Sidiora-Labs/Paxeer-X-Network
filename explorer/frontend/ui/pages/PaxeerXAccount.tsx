import { Box, chakra, Flex, Grid, Text } from '@chakra-ui/react';
import { pickBy } from 'es-toolkit';
import { useRouter } from 'next/router';
import React from 'react';

import type { PaxeerXCapabilities } from 'types/api/paxeerX';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import ActivityList from 'ui/paxeerX/account/ActivityList';
import AssetList from 'ui/paxeerX/account/AssetList';
import IdentityList from 'ui/paxeerX/account/IdentityList';
import { UNIFIED_ACCOUNT_PLACEHOLDER } from 'ui/paxeerX/account/placeholderData';
import { listIdentities } from 'ui/paxeerX/account/utils';
import TextAd from 'ui/shared/ad/TextAd';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import AddressIdenticon from 'ui/shared/entities/address/AddressIdenticon';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import IconSvg from 'ui/shared/IconSvg';
import PageTitle from 'ui/shared/Page/PageTitle';
import type { ScanSectionTabItem } from 'ui/shared/scan';
import { ScanKeyValue, ScanSectionTabs } from 'ui/shared/scan';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

const feature = config.features.paxeerXLists;
const apiDocsFeature = config.features.apiDocs;

const CAPABILITY_ROWS: Array<{ key: keyof PaxeerXCapabilities; label: string; hint: string }> = [
  { key: 'addr', label: 'Account binding', hint: 'Whether this node answers the account binding precompile that spells one account four ways' },
  { key: 'custody', label: 'Custody', hint: 'Whether this node answers the custody precompile that holds balances outside the chain state' },
  { key: 'anchor', label: 'Anchoring', hint: 'Whether this node answers the anchor precompile that settles kernel checkpoints on chain' },
];

// The probe answers three booleans; until it has answered, the card says nothing either way.
const capabilityLabel = (capabilities: PaxeerXCapabilities | undefined, key: keyof PaxeerXCapabilities) => {
  if (capabilities === undefined) {
    return '—';
  }

  return capabilities[key] ? 'Available' : 'Not available';
};

interface CardProps {
  title: string;
  children: React.ReactNode;
}

const DetailsCard = ({ title, children }: CardProps) => (
  <Box
    data-account-card={ title }
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

const PaxeerXAccountPageContent = () => {
  const router = useRouter();
  const hash = getQueryParamString(router.query.hash);
  const tab = getQueryParamString(router.query.tab);

  const capabilitiesQuery = useApiQuery('general:paxeer_x_capabilities', {
    queryOptions: {
      enabled: feature.isEnabled,
    },
  });

  const accountQuery = useApiQuery('general:paxeer_x_unified_account', {
    pathParams: { hash },
    queryOptions: {
      enabled: feature.isEnabled && Boolean(hash),
      placeholderData: UNIFIED_ACCOUNT_PLACEHOLDER,
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

  const isLoading = accountQuery.isPlaceholderData;
  const capabilities = capabilitiesQuery.data;
  const data = accountQuery.data;

  if (!feature.isEnabled) {
    return null;
  }

  const identities = data ? listIdentities(data.identities) : [];
  const latestActivity = data?.activity[0];

  const summary = data ? (
    <Grid
      data-account-details
      templateColumns={{ base: 'minmax(0, 1fr)', lg: 'repeat(3, minmax(0, 1fr))' }}
      gap={ 4 }
      mb={ 6 }
      alignItems="start"
    >
      <DetailsCard title="Overview">
        <ScanKeyValue label="Assets held" hint="Number of assets the account holds across the chain, the custody vault and the kernel" isLoading={ isLoading }>
          <Skeleton loading={ isLoading } data-field="assets-count">{ data.balances.length }</Skeleton>
        </ScanKeyValue>
        <ScanKeyValue label="Activity entries" hint="Number of chain-side and kernel-side entries in the account feed" isLoading={ isLoading }>
          <Skeleton loading={ isLoading } data-field="activity-count">{ data.activity.length }</Skeleton>
        </ScanKeyValue>
        <ScanKeyValue label="Identities" hint="Number of spellings of this account the node answers for" isLoading={ isLoading }>
          <Skeleton loading={ isLoading } data-field="identities-count">{ identities.length }</Skeleton>
        </ScanKeyValue>
      </DetailsCard>

      <DetailsCard title="More info">
        <ScanKeyValue label="Chain address" hint="The EVM address this account is bound to" isLoading={ isLoading } multiRow>
          { data.identities.evm ? (
            <AddressEntity address={{ hash: data.identities.evm }} isLoading={ isLoading } truncation="constant" noIcon/>
          ) : (
            <Text color="text.secondary" data-field="chain-address">—</Text>
          ) }
        </ScanKeyValue>
        <ScanKeyValue label="Latest activity" hint="Time of the most recent entry in the account feed" isLoading={ isLoading }>
          { latestActivity ? (
            <TimeWithTooltip timestamp={ latestActivity.timestamp } isLoading={ isLoading }/>
          ) : (
            <Text color="text.secondary" data-field="latest-activity">—</Text>
          ) }
        </ScanKeyValue>
        <ScanKeyValue label="Latest block" hint="Block of the most recent entry in the account feed" isLoading={ isLoading }>
          { latestActivity ? (
            <BlockEntity number={ latestActivity.block_number } isLoading={ isLoading } truncation="none" noIcon/>
          ) : (
            <Text color="text.secondary" data-field="latest-block">—</Text>
          ) }
        </ScanKeyValue>
      </DetailsCard>

      <DetailsCard title="Node capabilities">
        { CAPABILITY_ROWS.map(({ key, label, hint }) => (
          <ScanKeyValue key={ key } label={ label } hint={ hint } isLoading={ capabilitiesQuery.isPending }>
            <Skeleton loading={ capabilitiesQuery.isPending } data-capability={ key }>
              { capabilityLabel(capabilities, key) }
            </Skeleton>
          </ScanKeyValue>
        )) }
      </DetailsCard>
    </Grid>
  ) : null;

  const tabs: Array<ScanSectionTabItem & { component: React.ReactNode }> = data ? [
    {
      id: 'identities',
      title: 'Identities',
      count: identities.length,
      component: capabilities && !capabilities.addr ? (
        <Box color="text.secondary" data-capability-notice="addr">The account binding precompile is not available on this node.</Box>
      ) : (
        <IdentityList identities={ data.identities } isLoading={ isLoading }/>
      ),
    },
    {
      id: 'assets',
      title: 'Assets',
      count: data.balances.length,
      component: (
        <Flex flexDir="column" rowGap={ 3 }>
          { capabilities && !capabilities.custody && (
            <Box color="text.secondary" data-capability-notice="custody">
              The custody precompile is not available on this node, so only chain balances are counted.
            </Box>
          ) }
          <AssetList items={ data.balances } isLoading={ isLoading }/>
        </Flex>
      ),
    },
    {
      id: 'activity',
      title: 'Activity',
      count: data.activity.length,
      component: <ActivityList items={ data.activity } isLoading={ isLoading }/>,
    },
  ] : [];

  const activeTab = tabs.find(({ id }) => id === tab) ?? tabs[0];

  const apiEntry = apiDocsFeature.isEnabled ? (
    <Link href={ route({ pathname: '/api-docs' }) } textStyle="sm" display="inline-flex" alignItems="center" data-api-entry>
      <IconSvg name="API" boxSize={ 4 } mr={ 1 }/>
      API
    </Link>
  ) : null;

  const titleAfter = (
    <Flex alignItems="center" columnGap={ 2 } ml={ 3 } minW={ 0 } data-account-identifier>
      <Skeleton loading={ isLoading } overflow="hidden" minW={ 0 }>
        <chakra.span textStyle="lg" color="text.secondary" wordBreak="break-all">{ hash }</chakra.span>
      </Skeleton>
      <CopyToClipboard text={ hash } isLoading={ isLoading }/>
    </Flex>
  );

  return (
    <>
      <TextAd mb={ 6 }/>
      <PageTitle
        title="Unified account"
        beforeTitle={ hash ? <AddressIdenticon size={ 30 } hash={ hash }/> : undefined }
        afterTitle={ titleAfter }
        isLoading={ isLoading }
      />
      { accountQuery.isError || !data ? <DataFetchAlert/> : (
        <>
          { summary }
          <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }}>
            <ScanSectionTabs
              items={ tabs.map(({ id, title, count }) => ({ id, title, count })) }
              value={ activeTab?.id ?? 'identities' }
              onValueChange={ handleTabChange }
              rightSlot={ apiEntry }
            />
            { activeTab?.component }
          </Flex>
        </>
      ) }
    </>
  );
};

export default PaxeerXAccountPageContent;
