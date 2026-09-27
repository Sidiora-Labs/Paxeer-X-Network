import React from 'react';

import type { TxsSocketType } from './socket/types';
import type { Transaction, TransactionsSortingField, TransactionsSortingValue } from 'types/api/transaction';

import config from 'configs/app';
import { AddressHighlightProvider } from 'lib/contexts/addressHighlight';
import { useMultichainContext } from 'lib/contexts/multichain';
import useInitialList from 'lib/hooks/useInitialList';
import useIsMobile from 'lib/hooks/useIsMobile';
import useLazyRenderedList from 'lib/hooks/useLazyRenderedList';
import { currencyUnits } from 'lib/units';
import { TableBody, TableColumnHeader, TableColumnHeaderSortable, TableHeader, TableHeaderSticky, TableRoot, TableRow } from 'toolkit/chakra/table';
import TimeFormatToggle from 'ui/shared/time/TimeFormatToggle';

import type { TxsTranslationQuery } from './noves/useDescribeTxs';
import TxsSocketNotice from './socket/TxsSocketNotice';
import TxsTableItem from './TxsTableItem';

type Props = {
  txs: Array<Transaction>;
  sort: TransactionsSortingValue;
  onSortToggle?: (field: TransactionsSortingField) => void;
  top: number;
  showBlockInfo: boolean;
  socketType?: TxsSocketType;
  currentAddress?: string;
  enableTimeIncrement?: boolean;
  isLoading?: boolean;
  stickyHeader?: boolean;
  translationQuery?: TxsTranslationQuery;
};

const TxsTable = ({
  txs,
  sort,
  onSortToggle,
  top,
  showBlockInfo,
  socketType,
  currentAddress,
  enableTimeIncrement,
  isLoading,
  stickyHeader = true,
  translationQuery,
}: Props) => {
  const { cutRef, renderedItemsNum } = useLazyRenderedList(txs, !isLoading);
  const initialList = useInitialList({
    data: txs ?? [],
    idFn: (item) => item.hash,
    enabled: !isLoading,
  });
  const multichainContext = useMultichainContext();
  const chainData = multichainContext?.chain;
  const isMobile = useIsMobile();

  const feeCurrency = config.UI.views.tx.hiddenFields?.fee_currency || config.chain.hasMultipleGasCurrencies ?
    '' :
    ' ' + currencyUnits.ether;

  const TableHeaderComponent = stickyHeader ? TableHeaderSticky : TableHeader;

  const showValue = !config.UI.views.tx.hiddenFields?.value;
  const showFee = !config.UI.views.tx.hiddenFields?.tx_fee;

  const columnNum = [ showBlockInfo, true, showValue, showFee ].filter(Boolean).length;
  const baseWidth = `${ 100 / columnNum }%`;

  return (
    <AddressHighlightProvider>
      <TableRoot variant="scan" minWidth={{ base: '1200px', lg: '1080px' }}>
        <TableHeaderComponent top={ stickyHeader ? top : undefined }>
          <TableRow>
            <TableColumnHeader width="48px"></TableColumnHeader>
            { chainData && <TableColumnHeader width="32px"></TableColumnHeader> }
            <TableColumnHeader width="200px">Transaction hash</TableColumnHeader>
            <TableColumnHeader width="150px">Action</TableColumnHeader>
            { showBlockInfo && (
              onSortToggle ? (
                <TableColumnHeaderSortable
                  width="110px"
                  sortField="block_number"
                  sortValue={ sort }
                  onSortToggle={ onSortToggle }
                >
                  Block
                </TableColumnHeaderSortable>
              ) : (
                <TableColumnHeader width="110px">Block</TableColumnHeader>
              )
            ) }
            <TableColumnHeader width="130px">
              Age
              <TimeFormatToggle/>
            </TableColumnHeader>
            <TableColumnHeader width={ columnNum <= 2 ? baseWidth : '300px' }>From / To</TableColumnHeader>
            { showValue && (
              onSortToggle ? (
                <TableColumnHeaderSortable
                  width={ baseWidth }
                  isNumeric
                  sortField="value"
                  sortValue={ sort }
                  onSortToggle={ onSortToggle }
                >
                  { `Amount ${ currencyUnits.ether }` }
                </TableColumnHeaderSortable>
              ) : (
                <TableColumnHeader width={ baseWidth } isNumeric>Amount</TableColumnHeader>
              )
            ) }
            { showFee && (
              onSortToggle ? (
                <TableColumnHeaderSortable
                  width={ baseWidth }
                  isNumeric
                  pr={ 5 }
                  sortField="fee"
                  sortValue={ sort }
                  onSortToggle={ onSortToggle }
                >
                  { `Txn fee${ feeCurrency }` }
                </TableColumnHeaderSortable>
              ) : (
                <TableColumnHeader width={ baseWidth } isNumeric pr={ 5 }>Txn fee</TableColumnHeader>
              )
            ) }
          </TableRow>
        </TableHeaderComponent>
        <TableBody>
          { socketType && <TxsSocketNotice type={ socketType } place="table" isLoading={ isLoading }/> }
          { txs.slice(0, renderedItemsNum).map((item, index) => {
            return (
              <TxsTableItem
                key={ item.hash + (isLoading ? index : '') }
                tx={ item }
                showBlockInfo={ showBlockInfo }
                currentAddress={ currentAddress }
                enableTimeIncrement={ enableTimeIncrement }
                isLoading={ isLoading }
                animation={ initialList.getAnimationProp(item) }
                chainData={ chainData }
                translationIsLoading={ translationQuery?.isLoading }
                translationData={ translationQuery?.data?.find(({ txHash }) => txHash.toLowerCase() === item.hash.toLowerCase()) }
                isMobile={ isMobile }
              />
            );
          }) }
        </TableBody>
      </TableRoot>
      <div ref={ cutRef }/>
    </AddressHighlightProvider>
  );
};

export default React.memo(TxsTable);
