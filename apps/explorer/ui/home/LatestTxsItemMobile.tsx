import { Box, Flex, HStack, Text } from '@chakra-ui/react';
import React from 'react';

import type { Transaction } from 'types/api/transaction';

import config from 'configs/app';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tag } from 'toolkit/chakra/tag';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import EntityTag from 'ui/shared/EntityTags/EntityTag';
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

const LatestTxsItemMobile = ({ tx, isLoading }: Props) => {
  const dataTo = tx.to ? tx.to : tx.created_contract;

  const protocolTag = tx.to?.metadata?.tags?.find(tag => tag.tagType === 'protocol');

  return (
    <Box
      data-latest-tx={ tx.hash }
      w="100%"
      px={ 3 }
      py={ 3 }
      borderBottomWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Flex alignItems="center" justifyContent="space-between" columnGap={ 2 }>
        <TxEntity
          isLoading={ isLoading }
          hash={ tx.hash }
          noIcon
          truncation="constant_long"
          textStyle="sm"
          fontWeight="500"
        />
        <TxAdditionalInfo tx={ tx } isMobile isLoading={ isLoading }/>
      </Flex>
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
      <Box mt={ 2 } data-label="tx-parties">
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
      <Flex mt={ 2 } alignItems="center" justifyContent="space-between" columnGap={ 2 } flexWrap="wrap" rowGap={ 2 }>
        <HStack gap={ 2 } data-label="tx-tags">
          <TxType types={ tx.transaction_types } isLoading={ isLoading }/>
          { tx.status !== 'ok' && <TxStatus status={ tx.status } errorText={ tx.status === 'error' ? tx.result : undefined } isLoading={ isLoading }/> }
          <TxWatchListTags tx={ tx } isLoading={ isLoading }/>
          { protocolTag && <EntityTag data={ protocolTag } isLoading={ isLoading } minW="0" noColors/> }
        </HStack>
        { !(config.UI.views.tx.hiddenFields?.value && config.UI.views.tx.hiddenFields?.tx_fee) && (
          <Box textAlign="right" data-label="tx-value">
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
      </Flex>
    </Box>
  );
};

export default React.memo(LatestTxsItemMobile);
