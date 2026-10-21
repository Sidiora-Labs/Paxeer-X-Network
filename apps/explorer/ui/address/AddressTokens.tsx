import { Box, Flex } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import getQueryParamString from 'lib/router/getQueryParamString';
import { ADDRESS_TOKEN_BALANCE_ERC_20 } from 'stubs/address';
import { generateListStub } from 'stubs/utils';
import { Link } from 'toolkit/chakra/link';
import RoutedTabs from 'toolkit/components/RoutedTabs/RoutedTabs';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanTableCard } from 'ui/shared/scan';

import AddressCollections from './tokens/AddressCollections';
import AddressNftDisplayTypeRadio from './tokens/AddressNftDisplayTypeRadio';
import AddressNFTs from './tokens/AddressNFTs';
import AddressNftTypeFilter from './tokens/AddressNftTypeFilter';
import ERC20Tokens from './tokens/ERC20Tokens';
import TokenBalances from './tokens/TokenBalances';
import useAddressNftQuery from './tokens/useAddressNftQuery';

type Props = {
  shouldRender?: boolean;
  isQueryEnabled?: boolean;
  tokensCount?: number;
};

const AddressTokens = ({ shouldRender = true, isQueryEnabled = true, tokensCount }: Props) => {
  const router = useRouter();
  const isMobile = useIsMobile();
  const isMounted = useIsMounted();

  const scrollRef = React.useRef<HTMLDivElement>(null);

  const tab = getQueryParamString(router.query.tab);
  const hash = getQueryParamString(router.query.hash);

  // on address details we have tokens requests for all token types, separately
  // react query can behave unexpectedly, when it already has data for ERC-20 (string type)
  // and we fetch it again with the array type
  // so if it's just one token type, we heed to keep it a string for queries compatibility
  const tokenTypesFilter = config.chain.additionalTokenTypes.length > 0 ? [ 'ERC-20', ...config.chain.additionalTokenTypes.map(item => item.id) ] : 'ERC-20';

  const erc20Query = useQueryWithPages({
    resourceName: 'general:address_tokens',
    pathParams: { hash },
    filters: { type: tokenTypesFilter },
    scrollRef,
    options: {
      enabled: isQueryEnabled && tab !== 'tokens_nfts',
      refetchOnMount: false,
      placeholderData: generateListStub<'general:address_tokens'>(ADDRESS_TOKEN_BALANCE_ERC_20, 10, { next_page_params: null }),
    },
  });

  const { nftsQuery, collectionsQuery, displayType: nftDisplayType, tokenTypes: nftTokenTypes, onDisplayTypeChange, onTokenTypesChange } = useAddressNftQuery({
    scrollRef,
    enabled: isQueryEnabled && tab === 'tokens_nfts',
    addressHash: hash,
  });

  if (!isMounted || !shouldRender) {
    return null;
  }

  const hasActiveFilters = Boolean(nftTokenTypes?.length);

  let pagination: PaginationParams | undefined;

  if (tab === 'tokens_nfts') {
    pagination = nftDisplayType === 'list' ? nftsQuery.pagination : collectionsQuery.pagination;
  } else {
    pagination = erc20Query.pagination;
  }

  const hasNftData =
    (!nftsQuery.isPlaceholderData && nftsQuery.data?.items.length) ||
    (!collectionsQuery.isPlaceholderData && collectionsQuery.data?.items.length);

  const isNftTab = tab !== 'tokens' && tab !== 'tokens_erc20';

  const viewAllRow = (
    <Flex
      data-view-all
      justifyContent="center"
      alignItems="center"
      px={ 4 }
      py={ 3 }
      borderTopWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Link href={ route({ pathname: '/tokens' }) } textStyle="xs" fontWeight="500" textTransform="uppercase">
        View all tokens →
      </Link>
    </Flex>
  );

  const erc20ItemsNum = erc20Query.data?.items.length;
  const erc20Title = formatScanTableCount(
    tokensCount !== undefined && erc20ItemsNum !== undefined && erc20ItemsNum < tokensCount ?
      { kind: 'latest', value: tokensCount, itemsName: 'token balances', shownValue: erc20ItemsNum } :
      { kind: 'total', value: tokensCount ?? erc20ItemsNum ?? 0, itemsName: 'token balances' },
  );

  const nftItemsNum = nftDisplayType === 'list' ? nftsQuery.data?.items.length : collectionsQuery.data?.items.length;
  const nftTitle = formatScanTableCount({
    kind: 'total',
    value: nftItemsNum ?? 0,
    itemsName: nftDisplayType === 'list' ? 'NFTs' : 'collections',
  });

  const nftActions = (
    <>
      { (hasNftData || hasActiveFilters) && (
        <AddressNftDisplayTypeRadio value={ nftDisplayType } onChange={ onDisplayTypeChange }/>
      ) }
      { (hasNftData || hasActiveFilters) && !(isMobile && pagination.isVisible) && (
        <AddressNftTypeFilter value={ nftTokenTypes } onChange={ onTokenTypesChange }/>
      ) }
    </>
  );

  const tabs = [
    {
      id: 'tokens_erc20',
      title: [
        `${ config.chain.tokenStandard }-20`,
        ...config.chain.additionalTokenTypes.map((item) => item.name),
      ].join(' & '),
      component: (
        <ScanTableCard
          title={ erc20Title }
          pagination={ !isMobile ? <Pagination { ...erc20Query.pagination }/> : null }
        >
          <ERC20Tokens
            items={ erc20Query.data?.items }
            isLoading={ erc20Query.isPlaceholderData }
            pagination={ erc20Query.pagination }
            isError={ erc20Query.isError }
          />
          { viewAllRow }
        </ScanTableCard>
      ),
    },
    {
      id: 'tokens_nfts',
      title: 'NFTs',
      component: (
        <ScanTableCard
          title={ nftTitle }
          actions={ isNftTab ? nftActions : null }
          pagination={ !isMobile ? <Pagination { ...pagination }/> : null }
        >
          { nftDisplayType === 'list' ?
            <AddressNFTs tokensQuery={ nftsQuery } tokenTypes={ nftTokenTypes } onTokenTypesChange={ onTokenTypesChange }/> : (
              <AddressCollections
                collectionsQuery={ collectionsQuery }
                address={ hash }
                tokenTypes={ nftTokenTypes }
                onTokenTypesChange={ onTokenTypesChange }
              />
            ) }
          { viewAllRow }
        </ScanTableCard>
      ),
    },
  ];

  return (
    <>
      <TokenBalances/>
      { /* should stay before tabs to scroll up with pagination */ }
      <Box ref={ scrollRef }></Box>
      <RoutedTabs
        tabs={ tabs }
        variant="pill"
        size="sm"
      />
    </>
  );
};

export default AddressTokens;
