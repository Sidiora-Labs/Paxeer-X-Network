import { Box } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import type { TabItemRegular } from 'toolkit/components/AdaptiveTabs/types';
import type { TokenType } from 'types/api/token';
import type { TokensSortingValue, TokensSortingField, TokensSorting } from 'types/api/tokens';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import useDebounce from 'lib/hooks/useDebounce';
import getQueryParamString from 'lib/router/getQueryParamString';
import { TOKEN_INFO_ERC_20 } from 'stubs/token';
import { generateListStub } from 'stubs/utils';
import RoutedTabs from 'toolkit/components/RoutedTabs/RoutedTabs';
import PopoverFilter from 'ui/shared/filters/PopoverFilter';
import TokenTypeFilter from 'ui/shared/filters/TokenTypeFilter';
import PageTitle from 'ui/shared/Page/PageTitle';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { ScanShowRows } from 'ui/shared/scan';
import getSortParamsFromValue from 'ui/shared/sort/getSortParamsFromValue';
import getSortValueFromQuery from 'ui/shared/sort/getSortValueFromQuery';
import TokensList from 'ui/tokens/Tokens';
import TokensActionBar from 'ui/tokens/TokensActionBar';
import TokensBridgedChainsFilter from 'ui/tokens/TokensBridgedChainsFilter';
import { SORT_OPTIONS, getTokenFilterValue, getBridgedChainsFilterValue } from 'ui/tokens/utils';

// The list endpoint answers a fixed page, so the row selector chooses how much of that page the card
// shows and never promises a page size the API cannot serve.
const API_PAGE_SIZE = 50;
const ROWS_OPTIONS = [ 25, API_PAGE_SIZE ];

const bridgedTokensFeature = config.features.bridgedTokens;

const Tokens = () => {
  const router = useRouter();

  const tab = getQueryParamString(router.query.tab);
  const q = getQueryParamString(router.query.q);

  const [ searchTerm, setSearchTerm ] = React.useState<string>(q ?? '');
  const [ sort, setSort ] = React.useState<TokensSortingValue>(getSortValueFromQuery<TokensSortingValue>(router.query, SORT_OPTIONS) ?? 'default');
  const [ tokenTypes, setTokenTypes ] = React.useState<Array<TokenType> | undefined>(getTokenFilterValue(router.query.type));
  const [ bridgeChains, setBridgeChains ] = React.useState<Array<string> | undefined>(getBridgedChainsFilterValue(router.query.chain_ids));
  const [ rowsCount, setRowsCount ] = React.useState<number>(API_PAGE_SIZE);

  const debouncedSearchTerm = useDebounce(searchTerm, 300);

  const statsQuery = useApiQuery('general:stats', {
    queryOptions: {
      refetchOnMount: false,
      enabled: !config.UI.nativeCoinPrice.isHidden,
    },
  });

  const tokensQuery = useQueryWithPages({
    resourceName: tab === 'bridged' ? 'general:tokens_bridged' : 'general:tokens',
    filters: tab === 'bridged' ? { q: debouncedSearchTerm, chain_ids: bridgeChains } : { q: debouncedSearchTerm, type: tokenTypes },
    sorting: getSortParamsFromValue<TokensSortingValue, TokensSortingField, TokensSorting['order']>(sort),
    options: {
      placeholderData: generateListStub<'general:tokens'>(
        TOKEN_INFO_ERC_20,
        50,
        {
          next_page_params: {
            holders_count: 81528,
            items_count: 50,
            name: '',
            market_cap: null,
          },
        },
      ),
    },
  });

  const handleSearchTermChange = React.useCallback((value: string) => {
    tab === 'bridged' ?
      tokensQuery.onFilterChange({ q: value, chain_ids: bridgeChains }) :
      tokensQuery.onFilterChange({ q: value, type: tokenTypes });
    setSearchTerm(value);
  }, [ bridgeChains, tab, tokenTypes, tokensQuery ]);

  const handleTokenTypesChange = React.useCallback((value: Array<TokenType>) => {
    tokensQuery.onFilterChange({ q: debouncedSearchTerm, type: value });
    setTokenTypes(value);
  }, [ debouncedSearchTerm, tokensQuery ]);

  const handleBridgeChainsChange = React.useCallback((value: Array<string>) => {
    tokensQuery.onFilterChange({ q: debouncedSearchTerm, chain_ids: value });
    setBridgeChains(value);
  }, [ debouncedSearchTerm, tokensQuery ]);

  const handleSortChange = React.useCallback((value: TokensSortingValue) => {
    setSort(value);
    tokensQuery.onSortingChange(getSortParamsFromValue(value));
  }, [ tokensQuery ]);

  const handleTabChange = React.useCallback(() => {
    setSearchTerm('');
    setSort('default');
    setTokenTypes(undefined);
    setBridgeChains(undefined);
  }, []);

  const hasMultipleTabs = bridgedTokensFeature.isEnabled;

  const filter = tab === 'bridged' ? (
    <PopoverFilter contentProps={{ maxW: '350px' }} appliedFiltersNum={ bridgeChains?.length }>
      <TokensBridgedChainsFilter onChange={ handleBridgeChainsChange } defaultValue={ bridgeChains }/>
    </PopoverFilter>
  ) : (
    <PopoverFilter contentProps={{ w: '200px' }} appliedFiltersNum={ tokenTypes?.length }>
      <TokenTypeFilter<TokenType> onChange={ handleTokenTypesChange } defaultValue={ tokenTypes } nftOnly={ false }/>
    </PopoverFilter>
  );

  const actions = (
    <TokensActionBar
      key={ tab }
      filter={ filter }
      searchTerm={ searchTerm }
      onSearchChange={ handleSearchTermChange }
      sort={ sort }
      onSortChange={ handleSortChange }
    />
  );

  const pagination = <Pagination { ...tokensQuery.pagination }/>;

  const showRows = (
    <ScanShowRows
      value={ rowsCount }
      onValueChange={ setRowsCount }
      options={ ROWS_OPTIONS }
      isLoading={ tokensQuery.isPlaceholderData }
    />
  );

  const description = (() => {
    if (!bridgedTokensFeature.isEnabled) {
      return null;
    }

    const bridgesListText = bridgedTokensFeature.bridges.map((item, index, array) => {
      return item.title + (index < array.length - 2 ? ', ' : '') + (index === array.length - 2 ? ' and ' : '');
    });

    return (
      <Box textStyle="sm" mb={ 4 } mt={ 1 } whiteSpace="pre-wrap" flexWrap="wrap">
        List of the tokens bridged through { bridgesListText } extensions
      </Box>
    );
  })();

  const coinPrice = statsQuery.data?.coin_price;

  const renderList = (hasActiveFilters: boolean, listDescription?: React.ReactNode) => (
    <TokensList
      query={ tokensQuery }
      sort={ sort }
      onSortChange={ handleSortChange }
      hasActiveFilters={ hasActiveFilters }
      description={ listDescription }
      actions={ actions }
      pagination={ pagination }
      showRows={ showRows }
      rowsCount={ rowsCount }
      coinPrice={ coinPrice }
    />
  );

  const tabs: Array<TabItemRegular> = [
    {
      id: 'all',
      title: 'All',
      component: renderList(Boolean(searchTerm || tokenTypes)),
    },
    bridgedTokensFeature.isEnabled ? {
      id: 'bridged',
      title: 'Bridged',
      component: renderList(Boolean(searchTerm || bridgeChains), description),
    } : undefined,
  ].filter(Boolean);

  return (
    <>
      <PageTitle
        title={ config.meta.seo.enhancedDataEnabled ? `Tokens on ${ config.chain.name }` : 'Token tracker' }
        withTextAd
      />
      { hasMultipleTabs ? (
        <RoutedTabs
          tabs={ tabs }
          variant="pill"
          size="sm"
          onValueChange={ handleTabChange }
        />
      ) : renderList(Boolean(searchTerm || tokenTypes)) }
    </>
  );
};

export default Tokens;
