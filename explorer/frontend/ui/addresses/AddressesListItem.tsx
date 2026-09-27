import { Flex, HStack } from '@chakra-ui/react';
import type BigNumber from 'bignumber.js';
import React from 'react';

import type { AddressesItem } from 'types/api/addresses';

import { currencyUnits } from 'lib/units';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { ZERO } from 'toolkit/utils/consts';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import ListItemMobile from 'ui/shared/ListItemMobile/ListItemMobile';

import { AddressNameTag, getAddressBalance } from './AddressesTableItem';

type Props = {
  item: AddressesItem;
  index: number;
  totalSupply: BigNumber;
  isLoading?: boolean;
};

const AddressesListItem = ({
  item,
  index,
  totalSupply,
  isLoading,
}: Props) => {

  const addressBalance = getAddressBalance(item);

  const renderRow = (label: string, value: React.ReactNode) => (
    <HStack gap={ 3 } justifyContent="space-between" w="100%" alignItems="flex-start" data-address-field={ label }>
      <Skeleton loading={ isLoading } textStyle="sm" fontWeight={ 500 } flexShrink={ 0 }>{ label }</Skeleton>
      { value }
    </HStack>
  );

  return (
    <ListItemMobile rowGap={ 3 }>
      <Flex alignItems="center" columnGap={ 2 } w="100%">
        <Skeleton loading={ isLoading } textStyle="sm" color="text.secondary" minW={ 5 } data-address-rank>
          <span>{ index }</span>
        </Skeleton>
        <AddressEntity
          address={ item }
          isLoading={ isLoading }
          fontWeight={ 600 }
          truncation="constant"
        />
      </Flex>
      { renderRow('Name tag', <AddressNameTag item={ item } isLoading={ isLoading }/>) }
      { renderRow(`Balance ${ currencyUnits.ether }`, (
        <Skeleton loading={ isLoading } textStyle="sm" color="text.secondary" minW="0" whiteSpace="pre-wrap" textAlign="right">
          <span>{ addressBalance.dp(8).toFormat() }</span>
        </Skeleton>
      )) }
      { !totalSupply.eq(ZERO) && renderRow('Percentage', (
        <Skeleton loading={ isLoading } textStyle="sm" color="text.secondary">
          <span>{ addressBalance.div(totalSupply).multipliedBy(100).dp(8).toFormat() + '%' }</span>
        </Skeleton>
      )) }
      { renderRow('Txn count', (
        <Skeleton loading={ isLoading } textStyle="sm" color="text.secondary" data-address-txn-count>
          <span>{ Number(item.transactions_count).toLocaleString() }</span>
        </Skeleton>
      )) }
    </ListItemMobile>
  );
};

export default React.memo(AddressesListItem);
