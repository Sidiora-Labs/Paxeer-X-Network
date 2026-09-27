import { chakra, Flex, HStack } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import type { AddressesItem } from 'types/api/addresses';

import config from 'configs/app';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableCell, TableRow } from 'toolkit/chakra/table';
import { Tag } from 'toolkit/chakra/tag';
import AddressEntity from 'ui/shared/entities/address/AddressEntity';
import SimpleValue from 'ui/shared/value/SimpleValue';

type Props = {
  item: AddressesItem;
  index: number;
  totalSupply: BigNumber;
  hasPercentage: boolean;
  isLoading?: boolean;
};

export const ADDRESS_VALUE_PLACEHOLDER = '–';

export const getAddressBalance = (item: AddressesItem) =>
  BigNumber(item.coin_balance || 0).div(BigNumber(10 ** config.chain.currency.decimals));

export const AddressNameTag = ({ item, isLoading }: { item: AddressesItem; isLoading?: boolean }) => {
  const tags = item.public_tags ?? [];

  if (tags.length === 0) {
    return (
      <Skeleton loading={ isLoading } textStyle="sm" color="text.muted" display="inline-block" data-address-name-tag="none">
        <chakra.span>{ ADDRESS_VALUE_PLACEHOLDER }</chakra.span>
      </Skeleton>
    );
  }

  return (
    <HStack gap={ 1 } flexWrap="wrap" data-address-name-tag="tags">
      { tags.map((tag) => (
        <Tag key={ tag.label } loading={ isLoading } variant="outlined" truncated>{ tag.display_name }</Tag>
      )) }
    </HStack>
  );
};

const AddressesTableItem = ({
  item,
  index,
  totalSupply,
  hasPercentage,
  isLoading,
}: Props) => {

  const addressBalance = getAddressBalance(item);

  return (
    <TableRow data-address-row>
      <TableCell>
        <Skeleton loading={ isLoading } display="inline-block" minW={ 6 } color="text.secondary" lineHeight="24px" data-address-rank>
          { index }
        </Skeleton>
      </TableCell>
      <TableCell>
        <Flex alignItems="center" columnGap={ 2 }>
          <AddressEntity
            address={ item }
            isLoading={ isLoading }
            fontWeight={ 600 }
            my="2px"
          />
        </Flex>
      </TableCell>
      <TableCell>
        <AddressNameTag item={ item } isLoading={ isLoading }/>
      </TableCell>
      <TableCell isNumeric>
        <SimpleValue
          value={ addressBalance }
          loading={ isLoading }
          lineHeight="24px"
        />
      </TableCell>
      { hasPercentage && (
        <TableCell isNumeric>
          <SimpleValue
            value={ addressBalance.div(totalSupply).multipliedBy(100) }
            loading={ isLoading }
            postfix="%"
            lineHeight="24px"
          />
        </TableCell>
      ) }
      <TableCell isNumeric>
        <Skeleton loading={ isLoading } display="inline-block" color="text.secondary" lineHeight="24px" data-address-txn-count>
          { Number(item.transactions_count).toLocaleString() }
        </Skeleton>
      </TableCell>
    </TableRow>
  );
};

export default React.memo(AddressesTableItem);
