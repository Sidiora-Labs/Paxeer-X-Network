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
}

const PaxeerXReceiptDetails = ({ data, isLoading }: Props) => {
  return (
    <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }} data-receipt-details>
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
            <TxEntity
              hash={ data.transaction_hash }
              isLoading={ isLoading }
              truncation="none"
              noIcon
            />
          </ScanKeyValue>

          <ScanKeyValue
            label="Block"
            hint="The block that holds the emitting transaction"
            isLoading={ isLoading }
          >
            <BlockEntity
              number={ data.block_number }
              isLoading={ isLoading }
              truncation="none"
              noIcon
            />
          </ScanKeyValue>

          <ScanKeyValue
            label="Timestamp"
            hint="The time the block holding the emitting transaction was produced"
            isLoading={ isLoading }
          >
            <TimeWithTooltip
              timestamp={ data.timestamp }
              isLoading={ isLoading }
              timeFormat="absolute"
            />
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Box>
    </Flex>
  );
};

export default PaxeerXReceiptDetails;
