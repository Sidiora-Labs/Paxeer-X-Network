import { Box, chakra } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import getItemIndex from 'lib/getItemIndex';
import { currencyUnits } from 'lib/units';
import { TOP_ADDRESS } from 'stubs/address';
import { generateListStub } from 'stubs/utils';
import { Button } from 'toolkit/chakra/button';
import { ZERO } from 'toolkit/utils/consts';
import { saveAsCsv } from 'toolkit/utils/file';
import AddressesListItem from 'ui/addresses/AddressesListItem';
import AddressesTable from 'ui/addresses/AddressesTable';
import { getAddressBalance } from 'ui/addresses/AddressesTableItem';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import DataListDisplay from 'ui/shared/DataListDisplay';
import IconSvg from 'ui/shared/IconSvg';
import PageTitle from 'ui/shared/Page/PageTitle';
import Pagination from 'ui/shared/pagination/Pagination';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanShowRows, ScanTableCard } from 'ui/shared/scan';

// The list endpoint answers a fixed page, so the row selector chooses how much of that page the card
// shows and never promises a page size the API cannot serve.
const API_PAGE_SIZE = 50;
const ROWS_OPTIONS = [ 25, API_PAGE_SIZE ];
const BALANCE_ACCURACY = 3;

const Accounts = () => {
  const { isError, isPlaceholderData, data, pagination } = useQueryWithPages({
    resourceName: 'general:addresses',
    options: {
      placeholderData: generateListStub<'general:addresses'>(
        TOP_ADDRESS,
        50,
        {
          next_page_params: {
            fetched_coin_balance: '42',
            hash: '0x99f0ec06548b086e46cb0019c78d0b9b9f36cd53',
            items_count: 50,
          },
          total_supply: '0',
        },
      ),
    },
  });

  const [ rowsCount, setRowsCount ] = React.useState<number>(API_PAGE_SIZE);

  const pageStartIndex = getItemIndex(0, pagination.page);
  const totalSupply = React.useMemo(() => {
    return BigNumber(data?.total_supply || '0');
  }, [ data?.total_supply ]);

  const allItems = React.useMemo(() => data?.items ?? [], [ data?.items ]);
  const items = React.useMemo(() => allItems.slice(0, rowsCount), [ allItems, rowsCount ]);

  const handleDownloadClick = React.useCallback(() => {
    const hasPercentage = !totalSupply.eq(ZERO);

    const headerRows = [
      '#',
      'Address',
      'Name tag',
      `Balance ${ currencyUnits.ether }`,
      ...(hasPercentage ? [ 'Percentage' ] : []),
      'Txn count',
    ];

    const dataRows = items.map((item, index) => {
      const balance = getAddressBalance(item);

      return [
        String(pageStartIndex + index),
        item.hash,
        (item.public_tags ?? []).map((tag) => tag.display_name).join(' '),
        balance.toFixed(),
        ...(hasPercentage ? [ balance.div(totalSupply).multipliedBy(100).dp(8).toFixed() ] : []),
        String(item.transactions_count),
      ];
    });

    saveAsCsv(headerRows, dataRows, `top-accounts-page-${ pagination.page }.csv`);
  }, [ items, pageStartIndex, pagination.page, totalSupply ]);

  if (isError) {
    return (
      <>
        <PageTitle title="Top accounts" withTextAd/>
        <DataFetchAlert/>
      </>
    );
  }

  const countLine = formatScanTableCount({
    kind: pagination.hasNextPage ? 'more_than' : 'total',
    value: allItems.length === 0 ? 0 : getItemIndex(allItems.length - 1, pagination.page),
    itemsName: 'accounts',
  });

  const title = totalSupply.eq(ZERO) ?
    countLine :
    `${ countLine } (${ totalSupply.dp(BALANCE_ACCURACY).toFormat() } ${ currencyUnits.ether })`;

  const note = allItems.length > 0 ?
    `Showing ${ items.length.toLocaleString() } accounts ranked by balance` :
    undefined;

  const actions = (
    <Button
      variant="scan_control"
      size="sm"
      onClick={ handleDownloadClick }
      disabled={ isPlaceholderData || items.length === 0 }
      data-accounts-download
    >
      <IconSvg name="files/csv" boxSize={ 5 }/>
      <chakra.span ml={ 1 } hideBelow="lg">Download Page Data</chakra.span>
    </Button>
  );

  const showRows = (
    <ScanShowRows
      value={ rowsCount }
      onValueChange={ setRowsCount }
      options={ ROWS_OPTIONS }
      isLoading={ isPlaceholderData }
    />
  );

  const content = data?.items ? (
    <>
      <Box hideBelow="lg">
        <AddressesTable
          items={ items }
          totalSupply={ totalSupply }
          pageStartIndex={ pageStartIndex }
          isLoading={ isPlaceholderData }
        />
      </Box>
      <Box hideFrom="lg">
        { items.map((item, index) => {
          return (
            <AddressesListItem
              key={ item.hash + (isPlaceholderData ? index : '') }
              item={ item }
              index={ pageStartIndex + index }
              totalSupply={ totalSupply }
              isLoading={ isPlaceholderData }
            />
          );
        }) }
      </Box>
    </>
  ) : null;

  return (
    <>
      <PageTitle title="Top accounts" withTextAd/>
      <ScanTableCard
        title={ title }
        note={ note }
        actions={ actions }
        pagination={ <Pagination { ...pagination }/> }
        showRows={ showRows }
      >
        <DataListDisplay
          isError={ isError }
          itemsNum={ items.length }
          emptyText="There are no accounts."
        >
          { content }
        </DataListDisplay>
      </ScanTableCard>
    </>
  );
};

export default Accounts;
