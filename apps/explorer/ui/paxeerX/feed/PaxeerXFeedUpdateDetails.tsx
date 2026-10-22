import { Box, Flex } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import type { PriceUpdateItem } from 'types/api/paxeerXFeed';

import CopyToClipboard from 'ui/shared/CopyToClipboard';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import { ScanKeyValue, ScanTableCard } from 'ui/shared/scan';
import TxStatus from 'ui/shared/statusTag/TxStatus';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

import PaxeerXFeedObservationsTable from './PaxeerXFeedObservationsTable';

interface Props {
  data: PriceUpdateItem;
  isLoading?: boolean;
}

const PaxeerXFeedUpdateDetails = ({ data, isLoading }: Props) => {
  const summary = data.price_update;
  const observations = [ ...summary.observations ].sort((a, b) => a.market_id - b.market_id);

  return (
    <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }} data-price-update={ data.hash }>
      <Box
        bg="bg.surface"
        borderWidth="1px"
        borderStyle="solid"
        borderColor="border.divider"
        borderRadius="md"
        boxShadow="card"
        px={{ base: 4, lg: 6 }}
        py={{ base: 4, lg: 5 }}
      >
        <DetailedInfo.Container>
          <ScanKeyValue label="Update hash" hint="The hash of the price submission transaction" isLoading={ isLoading }>
            <Box overflow="hidden" textOverflow="ellipsis" data-field="hash">{ data.hash }</Box>
            <CopyToClipboard text={ data.hash } isLoading={ isLoading }/>
          </ScanKeyValue>

          <ScanKeyValue label="Status" hint="Whether the chain accepted the submission" isLoading={ isLoading }>
            <TxStatus status={ data.status } isLoading={ isLoading }/>
          </ScanKeyValue>

          <ScanKeyValue label="Block" hint="The block that carries the submission" isLoading={ isLoading } withDivider>
            <BlockEntity number={ data.block_number } isLoading={ isLoading } truncation="none" noIcon/>
          </ScanKeyValue>

          <ScanKeyValue label="Timestamp" hint="The time the block was produced" isLoading={ isLoading }>
            <TimeWithTooltip timestamp={ data.timestamp } isLoading={ isLoading } timeFormat="absolute"/>
          </ScanKeyValue>

          <ScanKeyValue label="Feeder" hint="The account that submitted the prices" isLoading={ isLoading } withDivider>
            <Flex flexDir="column" rowGap={ 1 } minW={ 0 }>
              <AddressEntity address={{ hash: data.from.hash }} isLoading={ isLoading } truncation="constant" noIcon/>
              <Box color="text.secondary" overflow="hidden" textOverflow="ellipsis" data-field="feeder_bech32">{ summary.feeder_bech32 }</Box>
            </Flex>
          </ScanKeyValue>

          <ScanKeyValue label="Gas used" hint="Gas used by the submission out of its gas limit" isLoading={ isLoading }>
            <Box data-field="gas">
              { new BigNumber(data.gas_used).toFormat() } of { new BigNumber(data.gas_limit).toFormat() }
            </Box>
          </ScanKeyValue>

          <ScanKeyValue label="Markets" hint="How many market prices the submission carries" isLoading={ isLoading }>
            <Box data-field="observation_count">{ summary.observation_count }</Box>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Box>

      <ScanTableCard title="Observations" note="Prices are shown at 18 decimals, ordered by market id">
        <PaxeerXFeedObservationsTable items={ observations }/>
      </ScanTableCard>
    </Flex>
  );
};

export default PaxeerXFeedUpdateDetails;
