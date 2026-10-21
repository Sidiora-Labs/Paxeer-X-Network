import {
  HStack,
  Flex,
} from '@chakra-ui/react';
import React from 'react';

import type { NovesDescribeTxsResponse } from 'types/api/noves';
import type { Transaction } from 'types/api/transaction';
import type { ClusterChainConfig } from 'types/multichain';

import config from 'configs/app';
import { Skeleton } from 'toolkit/chakra/skeleton';
import AddressFromTo from 'ui/shared/address/AddressFromTo';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import EntityTag from 'ui/shared/EntityTags/EntityTag';
import ListItemMobile from 'ui/shared/ListItemMobile/ListItemMobile';
import { ScanMethodChip } from 'ui/shared/scan';
import TxStatus from 'ui/shared/statusTag/TxStatus';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';
import TxFee from 'ui/shared/tx/TxFee';
import TxWatchListTags from 'ui/shared/tx/TxWatchListTags';
import NativeCoinValue from 'ui/shared/value/NativeCoinValue';
import TxAdditionalInfo from 'ui/txs/TxAdditionalInfo';
import TxType from 'ui/txs/TxType';

import TxTranslationType from './TxTranslationType';

const SELECTOR_LENGTH = 10;

type Props = {
  tx: Transaction;
  showBlockInfo: boolean;
  currentAddress?: string;
  enableTimeIncrement?: boolean;
  isLoading?: boolean;
  animation?: string;
  chainData?: ClusterChainConfig;
  translationIsLoading?: boolean;
  translationData?: NovesDescribeTxsResponse;
};

const TxsListItem = ({
  tx,
  isLoading,
  showBlockInfo,
  currentAddress,
  enableTimeIncrement,
  animation,
  chainData,
  translationIsLoading,
  translationData,
}: Props) => {
  const dataTo = tx.to ? tx.to : tx.created_contract;

  const protocolTag = tx.to?.hash !== currentAddress && tx.to?.metadata?.tags?.find(tag => tag.tagType === 'protocol');

  const method = tx.method ?? (tx.raw_input && tx.raw_input.length >= SELECTOR_LENGTH ? tx.raw_input.slice(0, SELECTOR_LENGTH) : undefined);

  return (
    <ListItemMobile display="block" width="100%" animation={ animation } key={ tx.hash }>
      <Flex justifyContent="space-between" alignItems="flex-start" mt={ 4 }>
        <HStack flexWrap="wrap">
          { translationIsLoading || translationData ? (
            <TxTranslationType
              txTypes={ tx.transaction_types }
              isLoading={ isLoading || translationIsLoading }
              type={ translationData?.type }
            />
          ) :
            <TxType types={ tx.transaction_types } isLoading={ isLoading }/>
          }
          { tx.status !== 'ok' && <TxStatus status={ tx.status } errorText={ tx.status === 'error' ? tx.result : undefined } isLoading={ isLoading }/> }
          <TxWatchListTags tx={ tx } isLoading={ isLoading }/>
          { protocolTag && <EntityTag data={ protocolTag } isLoading={ isLoading } noColors/> }
        </HStack>
        <TxAdditionalInfo tx={ tx } isMobile isLoading={ isLoading }/>
      </Flex>
      <Flex justifyContent="space-between" lineHeight="24px" mt={ 3 } alignItems="center">
        <TxEntity
          isLoading={ isLoading }
          hash={ tx.hash }
          truncation="constant_long"
          fontWeight="700"
          icon={ !tx.is_pending_update && tx.transaction_types.includes('blob_transaction') ? { name: 'blob' } : undefined }
          chain={ chainData }
          isPendingUpdate={ tx.is_pending_update }
        />
        <TimeWithTooltip
          timestamp={ tx.timestamp }
          enableIncrement={ enableTimeIncrement }
          isLoading={ isLoading }
          color="text.secondary"
          fontWeight="400"
          fontSize="sm"
        />
      </Flex>
      { method && (
        <Flex mt={ 3 } columnGap={ 2 } alignItems="center" minW={ 0 }>
          <Skeleton loading={ isLoading } display="inline-block" whiteSpace="pre">Method</Skeleton>
          <ScanMethodChip method={ method } isLoading={ isLoading }/>
        </Flex>
      ) }
      { showBlockInfo && tx.block_number !== null && (
        <Flex mt={ 2 }>
          <Skeleton loading={ isLoading } display="inline-block" whiteSpace="pre">Block </Skeleton>
          <BlockEntity
            isLoading={ isLoading }
            number={ tx.block_number }
            noIcon
          />
        </Flex>
      ) }
      <AddressFromTo
        from={ tx.from }
        to={ dataTo }
        current={ currentAddress }
        isLoading={ isLoading }
        mt={ 6 }
        fontWeight="500"
      />
      { !config.UI.views.tx.hiddenFields?.value && (
        <Flex mt={ 2 } columnGap={ 2 }>
          <Skeleton loading={ isLoading } display="inline-block" whiteSpace="pre">Value</Skeleton>
          <NativeCoinValue
            amount={ tx.value }
            exchangeRate={ tx.exchange_rate }
            historicalExchangeRate={ tx.historic_exchange_rate }
            loading={ isLoading }
            color="text.secondary"
          />
        </Flex>
      ) }
      { !config.UI.views.tx.hiddenFields?.tx_fee && (
        <Flex mt={ 2 } mb={ 3 } columnGap={ 2 }>
          { (tx.stability_fee !== undefined || tx.fee.value !== null) && (
            <>
              <Skeleton loading={ isLoading } display="inline-block" whiteSpace="pre">Fee</Skeleton>
              <TxFee tx={ tx } loading={ isLoading } color="text.secondary"/>
            </>
          ) }
        </Flex>
      ) }
    </ListItemMobile>
  );
};

export default React.memo(TxsListItem);
