import { Box, Center, Flex, HStack, Text } from '@chakra-ui/react';
import React from 'react';

import type { Transaction } from 'types/api/transaction';

import config from 'configs/app';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tag } from 'toolkit/chakra/tag';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import EntityTag from 'ui/shared/EntityTags/EntityTag';
import IconSvg from 'ui/shared/IconSvg';
import TxStatus from 'ui/shared/statusTag/TxStatus';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';
import TxFee from 'ui/shared/tx/TxFee';
import TxWatchListTags from 'ui/shared/tx/TxWatchListTags';
import NativeCoinValue from 'ui/shared/value/NativeCoinValue';
import TxAdditionalInfo from 'ui/txs/TxAdditionalInfo';
import TxType from 'ui/txs/TxType';

type Props = {
  tx: Transaction;
  isLoading?: boolean;
};

const LatestTxsItem = ({ tx, isLoading }: Props) => {
  const dataTo = tx.to ? tx.to : tx.created_contract;

  const protocolTag = tx.to?.metadata?.tags?.find(tag => tag.tagType === 'protocol');

  return (
    <Flex
      data-latest-tx={ tx.hash }
      alignItems="center"
      columnGap={ 3 }
      px={ 4 }
      py={ 3 }
      borderBottomWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Center
        boxSize={ 9 }
        flexShrink={ 0 }
        borderRadius="md"
        borderWidth="1px"
        borderStyle="solid"
        borderColor="border.divider"
      >
        <IconSvg name="transactions" boxSize={ 5 } color="icon.secondary" isLoading={ isLoading }/>
      </Center>
      <Box minW={ 0 } w={{ lg: '180px', xl: '210px' }} flexShrink={ 0 }>
        <TxEntity
          isLoading={ isLoading }
          hash={ tx.hash }
          noIcon
          textStyle="sm"
          fontWeight="500"
        />
        <TimeWithTooltip
          timestamp={ tx.timestamp }
          enableIncrement={ !isLoading }
          timeFormat="relative"
          isLoading={ isLoading }
          color="text.secondary"
          textStyle="xs"
          display="block"
          mt="2px"
        />
      </Box>
      <Box minW={ 0 } flexGrow={ 1 } data-label="tx-parties">
        <Flex alignItems="center" columnGap={ 1 } minW={ 0 }>
          <Skeleton loading={ isLoading } textStyle="xs" color="text.muted" flexShrink={ 0 }>From</Skeleton>
          <AddressEntity address={ tx.from } isLoading={ isLoading } noIcon truncation="constant" textStyle="xs"/>
        </Flex>
        { dataTo && (
          <Flex alignItems="center" columnGap={ 1 } minW={ 0 } mt="2px">
            <Skeleton loading={ isLoading } textStyle="xs" color="text.muted" flexShrink={ 0 }>To</Skeleton>
            <AddressEntity address={ dataTo } isLoading={ isLoading } noIcon truncation="constant" textStyle="xs"/>
          </Flex>
        ) }
      </Box>
      <HStack flexShrink={ 0 } gap={ 2 } data-label="tx-tags">
        <TxType types={ tx.transaction_types } isLoading={ isLoading }/>
        { tx.status !== 'ok' && <TxStatus status={ tx.status } errorText={ tx.status === 'error' ? tx.result : undefined } isLoading={ isLoading }/> }
        <TxWatchListTags tx={ tx } isLoading={ isLoading }/>
        { protocolTag && <EntityTag data={ protocolTag } isLoading={ isLoading } minW="0" noColors/> }
      </HStack>
      { !(config.UI.views.tx.hiddenFields?.value && config.UI.views.tx.hiddenFields?.tx_fee) && (
        <Box flexShrink={ 0 } textAlign="right" data-label="tx-value">
          { !config.UI.views.tx.hiddenFields?.value && (
            <Tag variant="outlined" loading={ isLoading }>
              <NativeCoinValue amount={ tx.value } accuracy={ 5 } loading={ isLoading }/>
            </Tag>
          ) }
          { !config.UI.views.tx.hiddenFields?.tx_fee && (
            <Skeleton loading={ isLoading } display="flex" justifyContent="flex-end" whiteSpace="pre" textStyle="xs" color="text.muted" mt="2px">
              <Text as="span">Fee </Text>
              <TxFee tx={ tx } accuracy={ 5 } noUsd/>
            </Skeleton>
          ) }
        </Box>
      ) }
      <TxAdditionalInfo tx={ tx } isLoading={ isLoading } flexShrink={ 0 }/>
    </Flex>
  );
};

export default React.memo(LatestTxsItem);
