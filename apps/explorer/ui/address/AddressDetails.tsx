import { Box, chakra, Grid, Text } from '@chakra-ui/react';
import { useRouter } from 'next/router';
import React from 'react';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useApiQuery from 'lib/api/useApiQuery';
import throwOnResourceLoadError from 'lib/errors/throwOnResourceLoadError';
import getNetworkValidationActionText from 'lib/networks/getNetworkValidationActionText';
import getNetworkValidatorTitle from 'lib/networks/getNetworkValidatorTitle';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import AddressCounterItem from 'ui/address/details/AddressCounterItem';
import { listIdentities } from 'ui/paxeerX/account/utils';
import ServiceDegradationWarning from 'ui/shared/alerts/ServiceDegradationWarning';
import isCustomAppError from 'ui/shared/AppError/isCustomAppError';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import DataFetchAlert from 'ui/shared/DataFetchAlert';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import DetailedInfoSponsoredItem from 'ui/shared/DetailedInfo/DetailedInfoSponsoredItem';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import BlockEntity from 'ui/shared/entities/block/BlockEntity';
import TxEntity from 'ui/shared/entities/tx/TxEntity';
import { ScanKeyValue } from 'ui/shared/scan';
import ContractCreationStatus from 'ui/shared/statusTag/ContractCreationStatus';

import Address3rdPartyWidgets from './Address3rdPartyWidgets';
import useAddress3rdPartyWidgets from './address3rdPartyWidgets/useAddress3rdPartyWidgets';
import AddressAlternativeFormat from './details/AddressAlternativeFormat';
import AddressBalance from './details/AddressBalance';
import AddressCeloAccount from './details/AddressCeloAccount';
import AddressImplementations from './details/AddressImplementations';
import AddressNameInfo from './details/AddressNameInfo';
import AddressNetWorth from './details/AddressNetWorth';
import FilecoinActorTag from './filecoin/FilecoinActorTag';
import TokenSelect from './tokenSelect/TokenSelect';
import type { AddressCountersQuery } from './utils/useAddressCountersQuery';
import type { AddressQuery } from './utils/useAddressQuery';

const paxeerXFeature = config.features.paxeerXLists;

interface CardProps {
  title: string;
  children: React.ReactNode;
}

const DetailsCard = ({ title, children }: CardProps) => {
  return (
    <Box
      data-address-card={ title }
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={ 4 }
      py={ 4 }
      minW={ 0 }
    >
      <chakra.h2 textStyle="sm" fontWeight="600" color="text.primary" mb={ 3 } data-card-title>{ title }</chakra.h2>
      <DetailedInfo.Container
        templateColumns="minmax(0, 1fr)"
        columnGap={ 0 }
        rowGap={ 2 }
        textStyle="sm"
      >
        { children }
      </DetailedInfo.Container>
    </Box>
  );
};

interface Props {
  addressQuery: AddressQuery;
  countersQuery: AddressCountersQuery;
  isLoading?: boolean;
}

const AddressDetails = ({ addressQuery, countersQuery, isLoading }: Props) => {
  const router = useRouter();

  const addressHash = getQueryParamString(router.query.hash);

  const addressType = addressQuery.data?.is_contract && addressQuery.data?.proxy_type !== 'eip7702' ? 'contract' : 'eoa';
  const address3rdPartyWidgets = useAddress3rdPartyWidgets(addressType, addressQuery.isPlaceholderData);

  const unifiedAccountQuery = useApiQuery('general:paxeer_x_unified_account', {
    pathParams: { hash: addressHash },
    queryOptions: {
      enabled: paxeerXFeature.isEnabled && Boolean(addressHash),
    },
  });

  const error404Data = React.useMemo(() => ({
    hash: addressHash || '',
    is_contract: false,
    implementations: null,
    token: null,
    watchlist_address_id: null,
    watchlist_names: null,
    creation_transaction_hash: null,
    block_number_balance_updated_at: null,
    name: null,
    exchange_rate: null,
    coin_balance: null,
    has_tokens: true,
    has_token_transfers: true,
    has_validated_blocks: false,
    filecoin: undefined,
    celo: undefined,
    creator_filecoin_robust_address: null,
    creator_address_hash: null,
  }), [ addressHash ]);

  // error handling (except 404 codes)
  if (addressQuery.isError) {
    if (isCustomAppError(addressQuery.error)) {
      const is404Error = addressQuery.isError && 'status' in addressQuery.error && addressQuery.error.status === 404;
      if (!is404Error) {
        throwOnResourceLoadError(addressQuery);
      }
    } else {
      return <DataFetchAlert/>;
    }
  }

  const data = addressQuery.isError ? error404Data : addressQuery.data;

  if (!data) {
    return null;
  }

  const creatorAddressHash = data.creator_address_hash;

  const identities = unifiedAccountQuery.data ? listIdentities(unifiedAccountQuery.data.identities) : [];
  const hasKernelAccount = paxeerXFeature.isEnabled && identities.length > 0;

  const overviewCard = (
    <DetailsCard title="Overview">
      <AddressBalance data={ data } isLoading={ isLoading }/>
      { (config.features.multichainButton.isEnabled || (data.exchange_rate && data.has_tokens)) && (
        <ScanKeyValue
          label="Net worth"
          hint="Total net worth in USD of all tokens for the address"
          isLoading={ isLoading }
          multiRow
        >
          <AddressNetWorth addressData={ addressQuery.data } addressHash={ addressHash } isLoading={ isLoading }/>
        </ScanKeyValue>
      ) }
      { data.has_tokens && (
        <ScanKeyValue label="Token holdings" hint="All tokens in the account and total value" multiRow>
          { addressQuery.data ? <TokenSelect/> : <Box>0</Box> }
        </ScanKeyValue>
      ) }
    </DetailsCard>
  );

  const moreInfoCard = (
    <DetailsCard title="More info">
      <AddressNameInfo data={ data } isLoading={ isLoading }/>
      <ScanKeyValue
        label="Transactions"
        hint="Number of transactions related to this address"
        isLoading={ isLoading || countersQuery.isPlaceholderData }
      >
        { addressQuery.data ? (
          <AddressCounterItem
            prop="transactions_count"
            query={ countersQuery }
            address={ data.hash }
            isAddressQueryLoading={ addressQuery.isPlaceholderData }
            isDegradedData={ addressQuery.isDegradedData }
          />
        ) :
          0 }
      </ScanKeyValue>
      { data.has_token_transfers && (
        <ScanKeyValue
          label="Transfers"
          hint="Number of transfers to/from this address"
          isLoading={ isLoading || countersQuery.isPlaceholderData }
        >
          { addressQuery.data ? (
            <AddressCounterItem
              prop="token_transfers_count"
              query={ countersQuery }
              address={ data.hash }
              isAddressQueryLoading={ addressQuery.isPlaceholderData }
              isDegradedData={ addressQuery.isDegradedData }
            />
          ) :
            0 }
        </ScanKeyValue>
      ) }
      { countersQuery.data?.gas_usage_count && (
        <ScanKeyValue
          label="Gas used"
          hint="Gas used by the address"
          isLoading={ isLoading || countersQuery.isPlaceholderData }
          multiRow
        >
          { addressQuery.data ? (
            <AddressCounterItem
              prop="gas_usage_count"
              query={ countersQuery }
              address={ data.hash }
              isAddressQueryLoading={ addressQuery.isPlaceholderData }
              isDegradedData={ addressQuery.isDegradedData }
            />
          ) :
            0 }
        </ScanKeyValue>
      ) }
      { data.has_validated_blocks && (
        <ScanKeyValue
          label={ `Blocks ${ getNetworkValidationActionText() }` }
          hint={ `Number of blocks ${ getNetworkValidationActionText() } by this ${ getNetworkValidatorTitle() }` }
          isLoading={ isLoading || countersQuery.isPlaceholderData }
        >
          { addressQuery.data ? (
            <AddressCounterItem
              prop="validations_count"
              query={ countersQuery }
              address={ data.hash }
              isAddressQueryLoading={ addressQuery.isPlaceholderData }
              isDegradedData={ addressQuery.isDegradedData }
            />
          ) :
            0 }
        </ScanKeyValue>
      ) }
      { data.block_number_balance_updated_at && (
        <ScanKeyValue label="Last balance update" hint="Block number in which the address was updated" isLoading={ isLoading }>
          <BlockEntity number={ data.block_number_balance_updated_at } isLoading={ isLoading }/>
        </ScanKeyValue>
      ) }
      { data.creation_transaction_hash && creatorAddressHash && (
        <ScanKeyValue
          label={ data.is_contract ? 'Creator' : 'Funded by' }
          hint={ data.is_contract ? 'Transaction and address of creation' : 'Address and transaction that first funded this account' }
          isLoading={ isLoading }
          multiRow
        >
          <AddressEntity
            address={{ hash: creatorAddressHash, filecoin: { robust: data.creator_filecoin_robust_address } }}
            truncation="constant"
            noIcon
          />
          <Text whiteSpace="pre"> at txn </Text>
          <TxEntity hash={ data.creation_transaction_hash } truncation="constant" noIcon/>
          { data.creation_status && <ContractCreationStatus status={ data.creation_status } ml={{ base: 0, lg: 2 }}/> }
        </ScanKeyValue>
      ) }
    </DetailsCard>
  );

  const kernelCard = (
    <DetailsCard title="Paxeer X account">
      { identities.map((identity) => (
        <ScanKeyValue key={ identity.kind } label={ identity.label } multiRow>
          <Text wordBreak="break-all" whiteSpace="normal" data-identity={ identity.kind }>{ identity.value }</Text>
          <CopyToClipboard text={ identity.value }/>
        </ScanKeyValue>
      )) }
      <ScanKeyValue label="Unified account" hint="The kernel view of this address, with its identities, assets and activity">
        <Link href={ route({ pathname: '/paxeer-x/account/[hash]', query: { hash: addressHash } }) } data-kernel-account-link>
          View unified account
        </Link>
      </ScanKeyValue>
    </DetailsCard>
  );

  const contractInfoCard = (
    <DetailsCard title={ data.is_contract ? 'Contract info' : 'Other info' }>
      { data.celo?.account && <AddressCeloAccount data={ data.celo.account } isLoading={ isLoading }/> }
      <AddressAlternativeFormat isLoading={ isLoading } addressHash={ addressHash }/>
      { data.filecoin?.id && (
        <ScanKeyValue label="ID" hint="Short identifier of an address that may change with chain state updates">
          <Text>{ data.filecoin.id }</Text>
          <CopyToClipboard text={ data.filecoin.id }/>
        </ScanKeyValue>
      ) }
      { data.filecoin?.actor_type && (
        <ScanKeyValue label="Actor" hint="Identifies the purpose and behavior of the address on the Filecoin network">
          <FilecoinActorTag actorType={ data.filecoin.actor_type }/>
        </ScanKeyValue>
      ) }
      { (data.filecoin?.actor_type === 'evm' || data.filecoin?.actor_type === 'ethaccount') && data?.filecoin?.robust && (
        <ScanKeyValue
          label="Ethereum Address"
          hint="0x-style address to which the Filecoin address is assigned by the Ethereum Address Manager"
        >
          <AddressEntity address={{ hash: data.hash }} noIcon noLink/>
        </ScanKeyValue>
      ) }
      { !isLoading && data.is_contract && data.implementations && data.implementations.length > 0 && (
        <AddressImplementations
          data={ data.implementations }
          isLoading={ isLoading }
          proxyType={ data.proxy_type }
        />
      ) }
      { (address3rdPartyWidgets.isEnabled && address3rdPartyWidgets.items.length > 0) && (
        <ScanKeyValue
          label="Widgets"
          hint="Metrics provided by third party partners"
          isLoading={ address3rdPartyWidgets.configQuery.isPlaceholderData || addressQuery.isPlaceholderData }
          multiRow
        >
          <Address3rdPartyWidgets addressType={ addressType } isLoading={ addressQuery.isPlaceholderData }/>
        </ScanKeyValue>
      ) }
      <DetailedInfoSponsoredItem isLoading={ isLoading }/>
    </DetailsCard>
  );

  return (
    <>
      { addressQuery.isDegradedData && <ServiceDegradationWarning isLoading={ isLoading } mb={ 6 }/> }
      <Grid
        data-address-details
        templateColumns={{ base: 'minmax(0, 1fr)', lg: 'repeat(3, minmax(0, 1fr))' }}
        gap={ 4 }
        mb={ 6 }
        alignItems="start"
      >
        { overviewCard }
        { moreInfoCard }
        { hasKernelAccount ? kernelCard : contractInfoCard }
      </Grid>
    </>
  );
};

export default React.memo(AddressDetails);
