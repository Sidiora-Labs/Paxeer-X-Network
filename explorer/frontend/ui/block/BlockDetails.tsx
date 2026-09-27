import { Box, chakra, Flex, GridItem, Text } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import { capitalize } from 'es-toolkit';
import { useRouter } from 'next/router';
import React from 'react';

import { ZKSYNC_L2_TX_BATCH_STATUSES } from 'types/api/zkSyncL2';

import { route, routeParams } from 'nextjs/routes';

import config from 'configs/app';
import getBlockReward from 'lib/block/getBlockReward';
import { useMultichainContext } from 'lib/contexts/multichain';
import getNetworkValidatorTitle from 'lib/networks/getNetworkValidatorTitle';
import * as arbitrum from 'lib/rollups/arbitrum';
import { formatZkSyncL2TxnBatchStatus, layerLabels } from 'lib/rollups/utils';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { Tooltip } from 'toolkit/chakra/tooltip';
import { ZERO } from 'toolkit/utils/consts';
import { space } from 'toolkit/utils/htmlEntities';
import OptimisticL2TxnBatchDA from 'ui/shared/batch/OptimisticL2TxnBatchDA';
import BlockGasUsed from 'ui/shared/block/BlockGasUsed';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import DetailedInfoTimestamp from 'ui/shared/DetailedInfo/DetailedInfoTimestamp';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import BatchEntityL2 from 'ui/shared/entities/block/BatchEntityL2';
import BlockEntityL1 from 'ui/shared/entities/block/BlockEntityL1';
import TxEntityL1 from 'ui/shared/entities/tx/TxEntityL1';
import HashStringShortenDynamic from 'ui/shared/HashStringShortenDynamic';
import IconSvg from 'ui/shared/IconSvg';
import PrevNext from 'ui/shared/PrevNext';
import RawDataSnippet from 'ui/shared/RawDataSnippet';
import { ScanExpander, ScanKeyValue } from 'ui/shared/scan';
import StatusTag from 'ui/shared/statusTag/StatusTag';
import Utilization from 'ui/shared/Utilization/Utilization';
import GasPriceValue from 'ui/shared/value/GasPriceValue';
import NativeCoinValue from 'ui/shared/value/NativeCoinValue';
import { WEI } from 'ui/shared/value/utils';
import VerificationSteps from 'ui/shared/verificationSteps/VerificationSteps';
import ZkSyncL2TxnBatchHashesInfo from 'ui/txnBatches/zkSyncL2/ZkSyncL2TxnBatchHashesInfo';

import BlockDetailsBaseFeeCelo from './details/BlockDetailsBaseFeeCelo';
import BlockDetailsBlobInfo from './details/BlockDetailsBlobInfo';
import BlockDetailsZilliqaQuorumCertificate from './details/BlockDetailsZilliqaQuorumCertificate';
import type { BlockQuery } from './useBlockQuery';

const zkSyncVerificationSteps = ZKSYNC_L2_TX_BATCH_STATUSES.map(formatZkSyncL2TxnBatchStatus);

interface Props {
  query: BlockQuery;
}

const rollupFeature = config.features.rollup;

const GRID_TEMPLATE_COLUMNS = { base: 'minmax(0, 1fr)', lg: 'minmax(min-content, 200px) minmax(0, 1fr)' };

const BlockDetails = ({ query }: Props) => {
  const router = useRouter();
  const heightOrHash = getQueryParamString(router.query.height_or_hash);
  const multichainContext = useMultichainContext();

  const { data, isPlaceholderData } = query;

  const handlePrevNextClick = React.useCallback((direction: 'prev' | 'next') => {
    if (!data) {
      return;
    }

    const increment = direction === 'next' ? +1 : -1;
    const nextId = String(data.height + increment);

    router.push(routeParams({ pathname: '/block/[height_or_hash]', query: { height_or_hash: nextId } }, { chain: multichainContext?.chain }));
  }, [ data, multichainContext, router ]);

  if (!data) {
    return null;
  }

  const { totalReward, staticReward, burntFees, txFees } = getBlockReward(data);

  const validatorTitle = getNetworkValidatorTitle();

  const rewardBreakDown = (() => {
    if (rollupFeature.isEnabled || totalReward.isEqualTo(ZERO) || txFees.isEqualTo(ZERO) || burntFees.isEqualTo(ZERO)) {
      return null;
    }

    if (isPlaceholderData) {
      return <Skeleton loading w="525px" h="20px"/>;
    }

    return (
      <Text color="text.secondary" whiteSpace="break-spaces">
        <Tooltip content="Static block reward">
          <span>{ staticReward.dividedBy(WEI).toFixed() }</span>
        </Tooltip>
        { !txFees.isEqualTo(ZERO) && (
          <>
            { space }+{ space }
            <Tooltip content="Txn fees">
              <span>{ txFees.dividedBy(WEI).toFixed() }</span>
            </Tooltip>
          </>
        ) }
        { !burntFees.isEqualTo(ZERO) && (
          <>
            { space }-{ space }
            <Tooltip content="Burnt fees">
              <span>{ burntFees.dividedBy(WEI).toFixed() }</span>
            </Tooltip>
          </>
        ) }
      </Text>
    );
  })();

  const txsNum = (() => {
    const blockTxsNum = (
      <Link href={ route({ pathname: '/block/[height_or_hash]', query: { height_or_hash: heightOrHash, tab: 'txs' } }, multichainContext) }>
        { data.transactions_count } txn{ data.transactions_count === 1 ? '' : 's' }
      </Link>
    );

    const blockBlobTxsNum = (config.features.dataAvailability.isEnabled && data.blob_transactions_count) ? (
      <>
        <span> including </span>
        <Link href={ route({ pathname: '/block/[height_or_hash]', query: { height_or_hash: heightOrHash, tab: 'blob_txs' } }, multichainContext) }>
          { data.blob_transactions_count } blob txn{ data.blob_transactions_count === 1 ? '' : 's' }
        </Link>
      </>
    ) : null;

    return (
      <>
        { blockTxsNum }
        { blockBlobTxsNum }
        <span> in this block</span>
      </>
    );
  })();

  const blockTypeLabel = (() => {
    switch (data.type) {
      case 'reorg':
        return 'Reorg';
      case 'uncle':
        return 'Uncle';
      default:
        return 'Block';
    }
  })();

  const hasMoreDetails = Boolean(
    (rollupFeature.isEnabled && rollupFeature.type === 'zkSync' && data.zksync) ||
    data.blob_gas_price ||
    data.bitcoin_merged_mining_header ||
    data.bitcoin_merged_mining_coinbase_transaction ||
    data.bitcoin_merged_mining_merkle_proof ||
    data.hash_for_merged_mining ||
    data.height > 0 ||
    (rollupFeature.isEnabled && rollupFeature.type === 'arbitrum' && data.arbitrum?.send_count) ||
    !config.UI.views.block.hiddenFields?.nonce ||
    data.zilliqa,
  );

  return (
    <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }} data-block-details>
      <Box
        data-block-details-card
        bg="bg.surface"
        borderWidth="1px"
        borderStyle="solid"
        borderColor="border.divider"
        borderRadius="md"
        boxShadow="card"
        px={{ base: 4, lg: 6 }}
        py={{ base: 4, lg: 5 }}
      >
        <DetailedInfo.Container templateColumns={ GRID_TEMPLATE_COLUMNS }>
          <ScanKeyValue
            label={ `${ blockTypeLabel } height` }
            hint="The block height of a particular block is defined as the number of blocks preceding it in the blockchain"
            isLoading={ isPlaceholderData }
          >
            <Skeleton loading={ isPlaceholderData }>
              { data.height }
            </Skeleton>
            { data.height === 0 && <Text whiteSpace="pre"> - Genesis Block</Text> }
            <PrevNext
              ml={ 6 }
              onClick={ handlePrevNextClick }
              prevLabel="View previous block"
              nextLabel="View next block"
              isPrevDisabled={ data.height === 0 }
              isLoading={ isPlaceholderData }
            />
          </ScanKeyValue>

          { rollupFeature.isEnabled && rollupFeature.type === 'arbitrum' && data.arbitrum && (
            <ScanKeyValue
              label={ `${ layerLabels.parent } block height` }
              hint={ `The most recent ${ layerLabels.parent } block height as of this ${ layerLabels.current } block` }
              isLoading={ isPlaceholderData }
            >
              <BlockEntityL1 isLoading={ isPlaceholderData } number={ data.arbitrum.l1_block_number }/>
            </ScanKeyValue>
          ) }

          { rollupFeature.isEnabled && rollupFeature.type === 'arbitrum' && data.arbitrum && !config.UI.views.block.hiddenFields?.batch && (
            <ScanKeyValue label="Batch" hint="Batch number" isLoading={ isPlaceholderData }>
              { data.arbitrum.batch_number ?
                <BatchEntityL2 isLoading={ isPlaceholderData } number={ data.arbitrum.batch_number }/> :
                <Skeleton loading={ isPlaceholderData }>Pending</Skeleton> }
            </ScanKeyValue>
          ) }

          { rollupFeature.isEnabled && rollupFeature.type === 'optimistic' && data.optimism && !config.UI.views.block.hiddenFields?.batch && (
            <ScanKeyValue label="Batch" hint="Batch number" isLoading={ isPlaceholderData }>
              <Flex alignItems="center" columnGap={ 3 }>
                { data.optimism.number ?
                  <BatchEntityL2 isLoading={ isPlaceholderData } number={ data.optimism.number }/> :
                  <Skeleton loading={ isPlaceholderData }>Pending</Skeleton> }
                { data.optimism.batch_data_container && (
                  <OptimisticL2TxnBatchDA
                    container={ data.optimism.batch_data_container }
                    isLoading={ isPlaceholderData }
                  />
                ) }
              </Flex>
            </ScanKeyValue>
          ) }

          { rollupFeature.isEnabled && rollupFeature.type === 'zkSync' && data.zksync && !config.UI.views.block.hiddenFields?.batch && (
            <ScanKeyValue label="Batch" hint="Batch number" isLoading={ isPlaceholderData }>
              { data.zksync.batch_number ?
                <BatchEntityL2 isLoading={ isPlaceholderData } number={ data.zksync.batch_number }/> :
                <Skeleton loading={ isPlaceholderData }>Pending</Skeleton> }
            </ScanKeyValue>
          ) }

          { !config.UI.views.block.hiddenFields?.L1_status && rollupFeature.isEnabled &&
            ((rollupFeature.type === 'zkSync' && data.zksync) || (rollupFeature.type === 'arbitrum' && data.arbitrum)) && (
            <ScanKeyValue
              label="Status"
              hint="Status is the short interpretation of the batch lifecycle"
              isLoading={ isPlaceholderData }
            >
              { rollupFeature.type === 'zkSync' && data.zksync && (
                <VerificationSteps
                  steps={ zkSyncVerificationSteps }
                  currentStep={ formatZkSyncL2TxnBatchStatus(data.zksync.status) }
                  isLoading={ isPlaceholderData }
                />
              ) }
              { rollupFeature.type === 'arbitrum' && data.arbitrum && (
                <VerificationSteps
                  steps={ arbitrum.verificationSteps }
                  currentStep={ arbitrum.VERIFICATION_STEPS_MAP[data.arbitrum.status] }
                  currentStepPending={ arbitrum.getVerificationStepStatus(data.arbitrum) === 'pending' }
                  isLoading={ isPlaceholderData }
                />
              ) }
            </ScanKeyValue>
          ) }

          <ScanKeyValue
            label="Timestamp"
            hint="Date & time at which block was produced."
            isLoading={ isPlaceholderData }
          >
            <DetailedInfoTimestamp timestamp={ data.timestamp } isLoading={ isPlaceholderData }/>
          </ScanKeyValue>

          <ScanKeyValue
            label="Transactions"
            hint="The number of transactions in the block"
            isLoading={ isPlaceholderData }
          >
            <Skeleton loading={ isPlaceholderData }>
              { txsNum }
            </Skeleton>
          </ScanKeyValue>

          { config.features.beaconChain.isEnabled && Boolean(data.withdrawals_count) && (
            <ScanKeyValue
              label="Withdrawals"
              hint="The number of beacon withdrawals in the block"
              isLoading={ isPlaceholderData }
            >
              <Skeleton loading={ isPlaceholderData }>
                <Link
                  href={ route({ pathname: '/block/[height_or_hash]', query: { height_or_hash: heightOrHash, tab: 'withdrawals' } }, multichainContext) }
                >
                  { data.withdrawals_count } withdrawal{ data.withdrawals_count === 1 ? '' : 's' }
                </Link>
              </Skeleton>
            </ScanKeyValue>
          ) }

          { !config.UI.views.block.hiddenFields?.miner && (
            <ScanKeyValue
              label={ capitalize(validatorTitle) }
              hint="A block producer who successfully included the block onto the blockchain"
              isLoading={ isPlaceholderData }
            >
              <AddressEntity
                address={ data.miner }
                isLoading={ isPlaceholderData }
              />
            </ScanKeyValue>
          ) }

          { rollupFeature.isEnabled && rollupFeature.type === 'arbitrum' &&
            (data.arbitrum?.commitment_transaction.hash || data.arbitrum?.confirmation_transaction.hash) && (
            <>
              <DetailedInfo.ItemDivider/>
              { data.arbitrum?.commitment_transaction.hash && (
                <ScanKeyValue
                  label="Commitment tx"
                  hint={ `${ layerLabels.parent } transaction containing this batch commitment` }
                  isLoading={ isPlaceholderData }
                >
                  <TxEntityL1 hash={ data.arbitrum?.commitment_transaction.hash } isLoading={ isPlaceholderData }/>
                  { data.arbitrum?.commitment_transaction.status === 'finalized' && <StatusTag type="ok" text="Finalized" ml={ 2 }/> }
                </ScanKeyValue>
              ) }
              { data.arbitrum?.confirmation_transaction.hash && (
                <ScanKeyValue
                  label="Confirmation tx"
                  hint={ `${ layerLabels.parent } transaction containing confirmation of this batch` }
                  isLoading={ isPlaceholderData }
                >
                  <TxEntityL1 hash={ data.arbitrum?.confirmation_transaction.hash } isLoading={ isPlaceholderData }/>
                  { data.arbitrum?.commitment_transaction.status === 'finalized' && <StatusTag type="ok" text="Finalized" ml={ 2 }/> }
                </ScanKeyValue>
              ) }
            </>
          ) }

          <DetailedInfo.ItemDivider data-scan-divider/>

          <ScanKeyValue
            label="Hash"
            hint="The SHA256 hash of the block"
            isLoading={ isPlaceholderData }
          >
            <Flex alignItems="center" flexWrap="nowrap" minW={ 0 } w="100%">
              <Box overflow="hidden" data-hash>
                <HashStringShortenDynamic hash={ data.hash }/>
              </Box>
              <CopyToClipboard text={ data.hash } isLoading={ isPlaceholderData }/>
            </Flex>
          </ScanKeyValue>

          { !rollupFeature.isEnabled && !totalReward.isEqualTo(ZERO) && !config.UI.views.block.hiddenFields?.total_reward && (
            <ScanKeyValue
              label="Block reward"
              hint={
                `For each block, the ${ validatorTitle } is rewarded with a finite amount of ${ config.chain.currency.symbol || 'native token' } 
          on top of the fees paid for all transactions in the block`
              }
              isLoading={ isPlaceholderData }
              multiRow
            >
              <NativeCoinValue amount={ totalReward.toString() } accuracy={ 0 } loading={ isPlaceholderData } mr={ 1 }/>
              { rewardBreakDown }
            </ScanKeyValue>
          ) }

          { data.rewards
            ?.filter(({ type }) => type !== 'Validator Reward' && type !== 'Miner Reward')
            .map(({ type, reward }) => (
              <ScanKeyValue
                key={ type }
                label={ type }
                hint={ `Amount of distributed reward. ${ capitalize(validatorTitle) }s receive a static block reward + Tx fees + uncle fees` }
              >
                <NativeCoinValue amount={ reward.toString() } accuracy={ 0 }/>
              </ScanKeyValue>
            ))
          }

          { typeof data.zilliqa?.view === 'number' && (
            <ScanKeyValue
              label="View"
              hint="The iteration of the consensus round in which the block was proposed"
              isLoading={ isPlaceholderData }
            >
              <Skeleton loading={ isPlaceholderData }>
                { data.zilliqa.view }
              </Skeleton>
            </ScanKeyValue>
          ) }

          { data.difficulty && (
            <ScanKeyValue
              label="Difficulty"
              hint={ `Block difficulty for ${ validatorTitle }, used to calibrate block generation time` }
              isLoading={ isPlaceholderData }
            >
              <Box overflow="hidden" data-difficulty>
                <HashStringShortenDynamic hash={ BigNumber(data.difficulty).toFormat() }/>
              </Box>
            </ScanKeyValue>
          ) }

          { data.total_difficulty && (
            <ScanKeyValue
              label="Total difficulty"
              hint="Total difficulty of the chain until this block"
              isLoading={ isPlaceholderData }
            >
              <Box overflow="hidden" data-total-difficulty>
                <HashStringShortenDynamic hash={ BigNumber(data.total_difficulty).toFormat() }/>
              </Box>
            </ScanKeyValue>
          ) }

          { typeof data.size === 'number' && (
            <ScanKeyValue
              label="Size"
              hint="Size of the block in bytes"
              isLoading={ isPlaceholderData }
            >
              <Skeleton loading={ isPlaceholderData } data-size>
                { data.size.toLocaleString() } bytes
              </Skeleton>
            </ScanKeyValue>
          ) }

          <DetailedInfo.ItemDivider data-scan-divider/>

          { data.celo?.base_fee && <BlockDetailsBaseFeeCelo data={ data.celo.base_fee }/> }

          <ScanKeyValue
            label="Gas used"
            hint="The total gas amount used in the block and its percentage of gas filled in the block"
            isLoading={ isPlaceholderData }
          >
            <Skeleton loading={ isPlaceholderData } data-gas-used>
              { BigNumber(data.gas_used || 0).toFormat() }
            </Skeleton>
            <BlockGasUsed
              gasUsed={ data.gas_used || undefined }
              gasLimit={ data.gas_limit }
              isLoading={ isPlaceholderData }
              ml={ 4 }
              gasTarget={ data.gas_target_percentage || undefined }
            />
          </ScanKeyValue>

          <ScanKeyValue
            label="Gas limit"
            hint="Total gas limit provided by all transactions in the block"
            isLoading={ isPlaceholderData }
          >
            <Skeleton loading={ isPlaceholderData }>
              { BigNumber(data.gas_limit).toFormat() }
            </Skeleton>
          </ScanKeyValue>

          { data.minimum_gas_price && (
            <ScanKeyValue
              label="Minimum gas price"
              hint="The minimum gas price a transaction should have in order to be included in this block"
              isLoading={ isPlaceholderData }
            >
              <NativeCoinValue amount={ data.minimum_gas_price } units="gwei" loading={ isPlaceholderData }/>
            </ScanKeyValue>
          ) }

          { data.base_fee_per_gas && (
            <ScanKeyValue
              label="Base fee per gas"
              hint="Minimum fee required per unit of gas. Fee adjusts based on network congestion"
              isLoading={ isPlaceholderData }
              multiRow
            >
              <GasPriceValue
                amount={ data.base_fee_per_gas }
                loading={ isPlaceholderData }
              />
            </ScanKeyValue>
          ) }

          { !config.UI.views.block.hiddenFields?.burnt_fees && !burntFees.isEqualTo(ZERO) && (
            <ScanKeyValue
              label="Burnt fees"
              hint={
                `Amount of ${ config.chain.currency.symbol || 'native token' } burned from transactions included in the block. 
              Equals Block Base Fee per Gas * Gas Used`
              }
              isLoading={ isPlaceholderData }
              multiRow
            >
              <NativeCoinValue
                amount={ burntFees.toString() }
                accuracy={ 0 }
                loading={ isPlaceholderData }
                startElement={ <IconSvg name="flame" boxSize={ 5 } mr={{ base: 1, lg: 2 }} color="icon.primary" isLoading={ isPlaceholderData }/> }
                mr={ 4 }
              />
              { !txFees.isEqualTo(ZERO) && (
                <Tooltip content="Burnt fees / Txn fees * 100%">
                  <Utilization
                    value={ burntFees.dividedBy(txFees).toNumber() }
                    isLoading={ isPlaceholderData }
                  />
                </Tooltip>
              ) }
            </ScanKeyValue>
          ) }

          { data.priority_fee !== null && BigNumber(data.priority_fee).gt(ZERO) && (
            <ScanKeyValue
              label="Priority fee / Tip"
              hint="User-defined tips sent to validator for transaction priority/inclusion"
              isLoading={ isPlaceholderData }
            >
              <NativeCoinValue amount={ data.priority_fee.toString() } accuracy={ 0 } loading={ isPlaceholderData }/>
            </ScanKeyValue>
          ) }

          { typeof data.extra_data === 'string' && (
            <ScanKeyValue
              label="Extra data"
              hint="Any data the block producer chose to include in the block, as the chain stores it"
              isLoading={ isPlaceholderData }
              multiRow
            >
              <Skeleton loading={ isPlaceholderData } w="100%">
                <chakra.textarea
                  data-extra-data
                  aria-label="Extra data"
                  readOnly
                  value={ data.extra_data }
                  w="100%"
                  minH="120px"
                  px={ 4 }
                  py={ 3 }
                  textStyle="sm"
                  fontFamily="body"
                  whiteSpace="pre-wrap"
                  wordBreak="break-all"
                  bg="bg.sunken"
                  color="text.primary"
                  borderWidth="1px"
                  borderStyle="solid"
                  borderColor="border.divider"
                  borderRadius="md"
                />
              </Skeleton>
            </ScanKeyValue>
          ) }
        </DetailedInfo.Container>
      </Box>

      { hasMoreDetails && (
        <ScanExpander hint="The fields this block carries beyond the ones the overview shows">
          <DetailedInfo.Container templateColumns={ GRID_TEMPLATE_COLUMNS }>
            { rollupFeature.isEnabled && rollupFeature.type === 'zkSync' && data.zksync &&
              <ZkSyncL2TxnBatchHashesInfo data={ data.zksync } isLoading={ isPlaceholderData }/> }

            { !isPlaceholderData && <BlockDetailsBlobInfo data={ data }/> }

            { data.bitcoin_merged_mining_header && (
              <ScanKeyValue label="Bitcoin merged mining header" hint="Merged-mining field: Bitcoin header">
                <Flex alignItems="center" flexWrap="nowrap" minW={ 0 } w="100%">
                  <Box whiteSpace="nowrap" overflow="hidden">
                    <HashStringShortenDynamic hash={ data.bitcoin_merged_mining_header }/>
                  </Box>
                  <CopyToClipboard text={ data.bitcoin_merged_mining_header }/>
                </Flex>
              </ScanKeyValue>
            ) }

            { data.bitcoin_merged_mining_coinbase_transaction && (
              <ScanKeyValue label="Bitcoin merged mining coinbase transaction" hint="Merged-mining field: Coinbase transaction" multiRow>
                <RawDataSnippet
                  data={ data.bitcoin_merged_mining_coinbase_transaction }
                  isLoading={ isPlaceholderData }
                  showCopy={ false }
                  textareaMaxHeight="100px"
                  w="100%"
                />
              </ScanKeyValue>
            ) }

            { data.bitcoin_merged_mining_merkle_proof && (
              <ScanKeyValue label="Bitcoin merged mining Merkle proof" hint="Merged-mining field: Merkle proof" multiRow>
                <RawDataSnippet
                  data={ data.bitcoin_merged_mining_merkle_proof }
                  isLoading={ isPlaceholderData }
                  showCopy={ false }
                  textareaMaxHeight="100px"
                  w="100%"
                />
              </ScanKeyValue>
            ) }

            { data.hash_for_merged_mining && (
              <ScanKeyValue label="Hash for merged mining" hint="Merged-mining field: Rootstock block header hash">
                <Flex alignItems="center" flexWrap="nowrap" minW={ 0 } w="100%">
                  <Box whiteSpace="nowrap" overflow="hidden">
                    <HashStringShortenDynamic hash={ data.hash_for_merged_mining }/>
                  </Box>
                  <CopyToClipboard text={ data.hash_for_merged_mining }/>
                </Flex>
              </ScanKeyValue>
            ) }

            { data.height > 0 && (
              <ScanKeyValue label="Parent hash" hint="The hash of the block from which this block was generated">
                <Flex alignItems="center" flexWrap="nowrap" minW={ 0 } w="100%">
                  <Link
                    href={ route({ pathname: '/block/[height_or_hash]', query: { height_or_hash: String(data.height - 1) } }, multichainContext) }
                    overflow="hidden"
                    whiteSpace="nowrap"
                  >
                    <HashStringShortenDynamic
                      hash={ data.parent_hash }
                    />
                  </Link>
                  <CopyToClipboard text={ data.parent_hash }/>
                </Flex>
              </ScanKeyValue>
            ) }

            { rollupFeature.isEnabled && rollupFeature.type === 'arbitrum' && data.arbitrum && data.arbitrum.send_count && (
              <>
                <ScanKeyValue
                  label="Send count"
                  hint={ `The cumulative number of ${ layerLabels.current } to ${ layerLabels.parent } transactions as of this block` }
                  isLoading={ isPlaceholderData }
                >
                  { data.arbitrum.send_count.toLocaleString() }
                </ScanKeyValue>

                <ScanKeyValue
                  label="Send root"
                  hint={
                    `The root of the Merkle accumulator representing all ${ layerLabels.current } to ${ layerLabels.parent } transactions as of this block`
                  }
                  isLoading={ isPlaceholderData }
                >
                  { data.arbitrum.send_root }
                </ScanKeyValue>

                <ScanKeyValue
                  label="Delayed messages"
                  hint={ `The number of delayed ${ layerLabels.parent } to ${ layerLabels.current } messages read as of this block` }
                  isLoading={ isPlaceholderData }
                >
                  { data.arbitrum.delayed_messages.toLocaleString() }
                </ScanKeyValue>
              </>
            ) }

            { !config.UI.views.block.hiddenFields?.nonce && (
              <ScanKeyValue label="Nonce" hint="Block nonce is a value used during mining to demonstrate proof of work for a block">
                { data.nonce }
              </ScanKeyValue>
            ) }

            { data.zilliqa && (
              <>
                <DetailedInfo.ItemDivider/>
                <BlockDetailsZilliqaQuorumCertificate data={ data.zilliqa?.quorum_certificate }/>
                { data.zilliqa?.aggregate_quorum_certificate && (
                  <>
                    <GridItem colSpan={{ base: undefined, lg: 2 }} mt={{ base: 1, lg: 2 }}/>
                    <BlockDetailsZilliqaQuorumCertificate data={ data.zilliqa?.aggregate_quorum_certificate }/>
                  </>
                ) }
              </>
            ) }
          </DetailedInfo.Container>
        </ScanExpander>
      ) }
    </Flex>
  );
};

export default BlockDetails;
