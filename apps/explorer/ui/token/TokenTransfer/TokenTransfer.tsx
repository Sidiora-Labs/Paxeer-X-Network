import { Box } from '@chakra-ui/react';
import type { UseQueryResult } from '@tanstack/react-query';
import { useRouter } from 'next/router';
import React from 'react';

import type { SocketMessage } from 'lib/socket/types';
import type { TokenInfo, TokenInstance } from 'types/api/token';

import type { ResourceError } from 'lib/api/resources';
import useGradualIncrement from 'lib/hooks/useGradualIncrement';
import useIsMobile from 'lib/hooks/useIsMobile';
import useIsMounted from 'lib/hooks/useIsMounted';
import useSocketChannel from 'lib/socket/useSocketChannel';
import useSocketMessage from 'lib/socket/useSocketMessage';
import DataListDisplay from 'ui/shared/DataListDisplay';
import Pagination from 'ui/shared/pagination/Pagination';
import type { QueryWithPagesResult } from 'ui/shared/pagination/useQueryWithPages';
import { formatScanTableCount, ScanShowRows, ScanTableCard, SCAN_ROWS_PER_PAGE } from 'ui/shared/scan';
import * as SocketNewItemsNotice from 'ui/shared/SocketNewItemsNotice';
import TokenAdvancedFilterLink from 'ui/token/TokenAdvancedFilterLink';
import TokenTransferList from 'ui/token/TokenTransfer/TokenTransferList';
import TokenTransferTable from 'ui/token/TokenTransfer/TokenTransferTable';

const DEFAULT_ROWS_TO_SHOW = 50;

type Props = {
  transfersQuery: QueryWithPagesResult<'general:token_transfers'> | QueryWithPagesResult<'general:token_instance_transfers'>;
  tokenId?: string;
  tokenInstance?: TokenInstance;
  tokenQuery: UseQueryResult<TokenInfo, ResourceError<unknown>>;
  shouldRender?: boolean;
  transfersCount?: number;
};

const TokenTransfer = ({ transfersQuery, tokenId, tokenQuery, tokenInstance, shouldRender = true, transfersCount }: Props) => {
  const isMobile = useIsMobile();
  const isMounted = useIsMounted();
  const router = useRouter();
  const { isError, isPlaceholderData, data, pagination } = transfersQuery;
  const { data: token, isPlaceholderData: isTokenPlaceholderData, isError: isTokenError } = tokenQuery;

  const [ newItemsCount, setNewItemsCount ] = useGradualIncrement(0);
  const [ showSocketErrorAlert, setShowSocketErrorAlert ] = React.useState(false);
  const [ rowsToShow, setRowsToShow ] = React.useState(DEFAULT_ROWS_TO_SHOW);

  const handleNewTransfersMessage: SocketMessage.TokenTransfers['handler'] = (payload) => {
    setNewItemsCount(payload.token_transfer);
  };

  const handleSocketClose = React.useCallback(() => {
    setShowSocketErrorAlert(true);
  }, []);

  const handleSocketError = React.useCallback(() => {
    setShowSocketErrorAlert(true);
  }, []);

  const channel = useSocketChannel({
    topic: `tokens:${ router.query.hash?.toString().toLowerCase() }`,
    onSocketClose: handleSocketClose,
    onSocketError: handleSocketError,
    isDisabled: isPlaceholderData || isError || pagination.page !== 1,
  });
  useSocketMessage({
    channel,
    event: 'token_transfer',
    handler: handleNewTransfersMessage,
  });

  if (!isMounted || !shouldRender) {
    return null;
  }

  const isLoading = isPlaceholderData || isTokenPlaceholderData;
  const items = data?.items.slice(0, rowsToShow);

  const content = items && token ? (
    <>
      <Box display={{ base: 'none', lg: 'block' }}>
        <TokenTransferTable
          data={ items }
          top={ 0 }
          showSocketInfo={ pagination.page === 1 }
          showSocketErrorAlert={ showSocketErrorAlert }
          socketInfoNum={ newItemsCount }
          tokenId={ tokenId }
          token={ token }
          instance={ tokenInstance }
          isLoading={ isLoading }
        />
      </Box>
      <Box display={{ base: 'block', lg: 'none' }}>
        { pagination.page === 1 && (
          <SocketNewItemsNotice.Mobile
            num={ newItemsCount }
            showErrorAlert={ showSocketErrorAlert }
            type="token_transfer"
            isLoading={ isLoading }
          />
        ) }
        <TokenTransferList data={ items } tokenId={ tokenId } instance={ tokenInstance } isLoading={ isLoading }/>
      </Box>
    </>
  ) : null;

  const itemsNum = items?.length ?? 0;
  const title = formatScanTableCount((() => {
    if (transfersCount !== undefined) {
      return itemsNum < transfersCount ?
        { kind: 'latest' as const, value: transfersCount, itemsName: 'token transfers', shownValue: itemsNum } :
        { kind: 'total' as const, value: transfersCount, itemsName: 'token transfers' };
    }

    return pagination.hasNextPage ?
      { kind: 'more_than' as const, value: itemsNum, itemsName: 'token transfers' } :
      { kind: 'total' as const, value: itemsNum, itemsName: 'token transfers' };
  })());

  return (
    <ScanTableCard
      title={ title }
      actions={ !isMobile ? <TokenAdvancedFilterLink token={ token } isLoading={ isLoading }/> : null }
      pagination={ pagination.isVisible ? <Pagination { ...pagination }/> : null }
      showRows={ (
        <ScanShowRows
          value={ rowsToShow }
          onValueChange={ setRowsToShow }
          options={ SCAN_ROWS_PER_PAGE }
          label="Show"
          suffix="Records"
          isLoading={ isLoading }
        />
      ) }
    >
      <DataListDisplay
        isError={ isError || isTokenError }
        itemsNum={ itemsNum }
        emptyText="There are no token transfers."
      >
        { content }
      </DataListDisplay>
    </ScanTableCard>
  );
};

export default React.memo(TokenTransfer);
