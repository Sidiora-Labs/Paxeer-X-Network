import { Box, Flex, Text } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXReceipt, PaxeerXVerificationStatus } from 'types/api/paxeerXLists';

import CopyToClipboard from 'ui/shared/CopyToClipboard';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import { ScanKeyValue } from 'ui/shared/scan';
import StatusLadderBadge from 'ui/shared/statusLadder/StatusLadderBadge';
import TimeWithTooltip from 'ui/shared/time/TimeWithTooltip';

export const VERIFICATION_STATUS_LABELS: Record<PaxeerXVerificationStatus, string> = {
  unverified: 'Unverified',
  sequencer_signed: 'Sequencer signed',
  batch_included: 'Batch included',
  state_proven: 'State proven',
  checkpoint_finalised: 'Checkpoint finalised',
  settlement_anchored: 'Settlement anchored',
};

interface Props {
  data: PaxeerXReceipt;
  isLoading?: boolean;
  freshness?: {
    state: 'current' | 'refreshing' | 'stale' | 'paused' | 'complete';
    checkedAt?: number;
    message?: string;
  };
}

const PaxeerXReceiptDetails = ({ data, isLoading, freshness }: Props) => {
  return (
    <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }} data-receipt-details
      data-settlement-status={ data.status } data-verification-status={ data.verification_status }>
      { freshness ? (
        <Box role="status" aria-live="polite" data-receipt-freshness={ freshness.state } color="text.secondary" textStyle="sm">
          <Text>{ freshness.message ?? {
            current: 'Latest indexed response received. Checking for updates.',
            refreshing: 'Checking for newer indexed evidence.',
            stale: 'Evidence may be stale. Retaining the last accepted response.',
            paused: 'Refresh paused while this page is hidden.',
            complete: 'Final settlement and anchored verification reported. Automatic refresh stopped.',
          }[freshness.state] }</Text>
          { freshness.checkedAt !== undefined ? (
            <Text>Last checked: <time dateTime={ new Date(freshness.checkedAt).toISOString() } data-receipt-checked-at>
              { new Date(freshness.checkedAt).toISOString() }
            </time></Text>
          ) : null }
          <Text>Settlement and verification are separate indexed evidence. Time since the last check does not advance either.</Text>
        </Box>
      ) : null }
      <Box
        data-receipt-details-card
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
          <ScanKeyValue
            label="Receipt ID"
            hint="The identifier the kernel receipt log records this receipt under"
            isLoading={ isLoading }
          >
            <Box overflow="hidden" textOverflow="ellipsis" data-field="id">{ data.id }</Box>
            <CopyToClipboard text={ data.id } isLoading={ isLoading }/>
          </ScanKeyValue>

          <ScanKeyValue
            label="Kernel account"
            hint="The kernel account the receipt was issued to"
            isLoading={ isLoading }
            withDivider
          >
            { data.account === null ? (
              <Text color="text.secondary" data-field="account">—</Text>
            ) : (
              <>
                <Box overflow="hidden" textOverflow="ellipsis" data-field="account">{ data.account }</Box>
                <CopyToClipboard text={ data.account } isLoading={ isLoading }/>
              </>
            ) }
          </ScanKeyValue>

          <ScanKeyValue
            label="Settlement status"
            hint="The rung the block holding this receipt has reached on the settlement ladder"
            isLoading={ isLoading }
          >
            <StatusLadderBadge rung={ data.status } isLoading={ isLoading }/>
          </ScanKeyValue>

          <ScanKeyValue
            label="Verification status"
            hint="The rung the receipt has reached on the kernel's own verification lattice"
            isLoading={ isLoading }
          >
            <Box data-field="verification_status">{ VERIFICATION_STATUS_LABELS[data.verification_status] }</Box>
          </ScanKeyValue>

          <ScanKeyValue
            label="Payload hash"
            hint="The hash of the payload the receipt commits to, when the receipt log records one"
            isLoading={ isLoading }
            withDivider
          >
            { data.payload_hash === null ? (
              <Text color="text.secondary" data-field="payload_hash">—</Text>
            ) : (
              <>
                <Box overflow="hidden" textOverflow="ellipsis" data-field="payload_hash">{ data.payload_hash }</Box>
                <CopyToClipboard text={ data.payload_hash } isLoading={ isLoading }/>
              </>
            ) }
          </ScanKeyValue>

          <ScanKeyValue
            label="Transaction"
            hint="The transaction that emitted the receipt"
            isLoading={ isLoading }
          >
            { data.transaction_hash === null ? <Text color="text.secondary">—</Text> : <TxEntity
              hash={ data.transaction_hash }
              isLoading={ isLoading }
              truncation="none"
              noIcon
            /> }
          </ScanKeyValue>

          <ScanKeyValue
            label="Block"
            hint="The block that holds the emitting transaction"
            isLoading={ isLoading }
          >
            { data.block_number === null ? <Text color="text.secondary">—</Text> : <BlockEntity
              number={ data.block_number }
              isLoading={ isLoading }
              truncation="none"
              noIcon
            /> }
          </ScanKeyValue>

          <ScanKeyValue
            label="Timestamp"
            hint="The time the block holding the emitting transaction was produced"
            isLoading={ isLoading }
          >
            { data.timestamp === null ? <Text color="text.secondary">—</Text> : <TimeWithTooltip
              timestamp={ data.timestamp }
              isLoading={ isLoading }
              timeFormat="absolute"
            /> }
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Box>
    </Flex>
  );
};

export default PaxeerXReceiptDetails;
