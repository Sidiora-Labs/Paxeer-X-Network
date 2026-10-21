import { chakra, Flex, useToken } from '@chakra-ui/react';
import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { Address } from 'types/api/address';
import type { TokenInfo, TokenVerifiedInfo as TTokenVerifiedInfo } from 'types/api/token';
import type { EntityTag } from 'ui/shared/EntityTags/types';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useAddressMetadataInfoQuery from 'lib/address/useAddressMetadataInfoQuery';
import type { ResourceError } from 'lib/api/resources';
import { useMultichainContext } from 'lib/contexts/multichain';
import { getTokenTypeName } from 'lib/token/tokenTypes';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tag } from 'toolkit/chakra/tag';
import { Tooltip } from 'toolkit/chakra/tooltip';
import AddressAlerts from 'ui/address/details/AddressAlerts';
import AddressQrCode from 'ui/address/details/AddressQrCode';
import AccountActionsMenu from 'ui/shared/AccountActionsMenu/AccountActionsMenu';
import AddressAddToWallet from 'ui/shared/address/AddressAddToWallet';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import * as TokenEntity from 'ui/shared/entities/token/TokenEntity';
import EntityTags from 'ui/shared/EntityTags/EntityTags';
import formatUserTags from 'ui/shared/EntityTags/formatUserTags';
import sortEntityTags from 'ui/shared/EntityTags/sortEntityTags';
import IconSvg from 'ui/shared/IconSvg';
import NetworkExplorers from 'ui/shared/NetworkExplorers';
import PageTitle from 'ui/shared/Page/PageTitle';
import { ScanMethodChip } from 'ui/shared/scan';

import TokenVerifiedInfo from './TokenVerifiedInfo';

const PREDEFINED_TAG_PRIORITY = 100;

const apiDocsFeature = config.features.apiDocs;

interface Props {
  tokenQuery: UseQueryResult<TokenInfo, ResourceError<unknown>>;
  addressQuery: UseQueryResult<Address, ResourceError<unknown>>;
  verifiedInfoQuery: UseQueryResult<TTokenVerifiedInfo, ResourceError<unknown>>;
  hash: string;
}

const TokenPageTitle = ({ tokenQuery, addressQuery, verifiedInfoQuery, hash }: Props) => {
  const multichainContext = useMultichainContext();
  const addressHash = !tokenQuery.isPlaceholderData ? (tokenQuery.data?.address_hash || '') : '';

  const addressesForMetadataQuery = React.useMemo(() => ([ hash ].filter(Boolean)), [ hash ]);
  const addressMetadataQuery = useAddressMetadataInfoQuery(addressesForMetadataQuery);

  const isLoading = tokenQuery.isPlaceholderData ||
    addressQuery.isPlaceholderData ||
    (config.features.verifiedTokens.isEnabled && verifiedInfoQuery.isPending);

  const tokenSymbolText = tokenQuery.data?.symbol ? ` (${ tokenQuery.data.symbol })` : '';

  const [ bridgedTokenTagBgColor ] = useToken('colors', 'blue.500');
  const [ bridgedTokenTagTextColor ] = useToken('colors', 'white');

  const tags: Array<EntityTag> = React.useMemo(() => {
    return [
      config.features.bridgedTokens.isEnabled && tokenQuery.data?.is_bridged ?
        {
          slug: 'bridged',
          name: 'Bridged',
          tagType: 'custom' as const,
          ordinal: PREDEFINED_TAG_PRIORITY,
          meta: { bgColor: bridgedTokenTagBgColor, textColor: bridgedTokenTagTextColor },
        } :
        undefined,
      ...formatUserTags(addressQuery.data),
      verifiedInfoQuery.data?.projectSector ?
        { slug: verifiedInfoQuery.data.projectSector, name: verifiedInfoQuery.data.projectSector, tagType: 'custom' as const, ordinal: -30 } :
        undefined,
      ...(addressMetadataQuery.data?.addresses?.[hash.toLowerCase()]?.tags.filter(tag => tag.tagType !== 'note') || []),
    ].filter(Boolean).sort(sortEntityTags);
  }, [
    addressMetadataQuery.data?.addresses,
    addressQuery.data,
    bridgedTokenTagBgColor,
    bridgedTokenTagTextColor,
    tokenQuery.data?.is_bridged,
    verifiedInfoQuery.data?.projectSector,
    hash,
  ]);

  const standard = tokenQuery.data ? getTokenTypeName(tokenQuery.data.type, multichainContext?.chain?.app_config) : undefined;
  const implementation = addressQuery.data?.implementations?.[0];

  const contentAfter = (
    <>
      <Skeleton loading={ tokenQuery.isPlaceholderData }>
        <chakra.span textStyle="lg" color="text.secondary" data-token-name>
          { `${ tokenQuery.data?.name || 'Unnamed token' }${ tokenSymbolText }` }
        </chakra.span>
      </Skeleton>
      { tokenQuery.data && <TokenEntity.Reputation value={ tokenQuery.data.reputation } ml={ 0 }/> }
      { verifiedInfoQuery.data?.tokenAddress && (
        <Tooltip content={ `Information on this token has been verified by ${ config.chain.name }` }>
          <IconSvg name="certified" color="green.500" boxSize={ 6 } cursor="pointer" data-token-verified/>
        </Tooltip>
      ) }
    </>
  );

  const apiEntry = apiDocsFeature.isEnabled ? (
    <Link
      href={ route({ pathname: '/api-docs' }, multichainContext) }
      data-token-api-link
      display="inline-flex"
      alignItems="center"
      columnGap={ 1 }
      textStyle="sm"
      fontWeight="500"
    >
      <IconSvg name="API" boxSize={ 5 }/>
      API
    </Link>
  ) : null;

  const chipRow = (
    <Flex
      data-token-chip-row
      alignItems="center"
      justifyContent="space-between"
      w="100%"
      minW={ 0 }
      columnGap={ 3 }
      rowGap={ 3 }
      flexWrap="wrap"
    >
      <Flex alignItems="center" minW={ 0 } columnGap={ 2 } rowGap={ 2 } flexWrap="wrap" data-token-chips>
        { standard && <ScanMethodChip method={ standard } isLoading={ tokenQuery.isPlaceholderData }/> }
        { addressQuery.data?.is_verified && (
          <ScanMethodChip
            method={ implementation ? 'Source Code (Proxy)' : 'Source Code' }
            isLoading={ addressQuery.isPlaceholderData }
          />
        ) }
        { implementation && (
          <Tag variant="outlined" label="Implementation" loading={ addressQuery.isPlaceholderData } data-token-implementation>
            <AddressEntity
              address={{ hash: implementation.address_hash, name: implementation.name ?? null }}
              isLoading={ addressQuery.isPlaceholderData }
              noIcon
              noCopy
              truncation="constant"
            />
          </Tag>
        ) }
        <EntityTags
          isLoading={ isLoading || (config.features.addressMetadata.isEnabled && addressMetadataQuery.isPending) }
          tags={ tags }
          addressHash={ addressQuery.data?.hash }
        />
      </Flex>
      <Flex alignItems="center" columnGap={ 2 } rowGap={ 2 } flexWrap="wrap" data-token-actions>
        <TokenVerifiedInfo verifiedInfoQuery={ verifiedInfoQuery }/>
        { apiEntry }
        { !isLoading && tokenQuery.data && <AddressAddToWallet token={ tokenQuery.data } variant="button"/> }
        { addressQuery.data && <AddressQrCode hash={ addressQuery.data.hash } isLoading={ isLoading }/> }
        <NetworkExplorers type="token" pathParam={ addressHash }/>
        <AccountActionsMenu isLoading={ isLoading }/>
      </Flex>
    </Flex>
  );

  return (
    <>
      <PageTitle
        title="Token"
        isLoading={ tokenQuery.isPlaceholderData }
        beforeTitle={ tokenQuery.data ? (
          <TokenEntity.Icon
            token={ tokenQuery.data }
            isLoading={ tokenQuery.isPlaceholderData }
            variant="heading"
            chain={ multichainContext?.chain }
          />
        ) : null }
        contentAfter={ contentAfter }
        secondRow={ chipRow }
      />
      { !addressMetadataQuery.isPending && (
        <AddressAlerts
          tags={ addressMetadataQuery.data?.addresses?.[hash.toLowerCase()]?.tags }
          isScamToken={ tokenQuery.data?.reputation === 'scam' }
        />
      ) }
    </>
  );
};

export default TokenPageTitle;
