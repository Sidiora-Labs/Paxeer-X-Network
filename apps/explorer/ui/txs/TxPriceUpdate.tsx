import { Flex } from '@chakra-ui/react';
import React from 'react';

import type { Transaction } from 'types/api/transaction';

import { route } from 'nextjs/routes';

import { Link } from 'toolkit/chakra/link';
import AddressFromTo from 'ui/shared/address/AddressFromTo';

export const isPriceUpdate = (tx: Pick<Transaction, 'transaction_types'>) => tx.transaction_types.includes('price_update');

export const priceUpdateHref = (hash: string) => route({ pathname: '/feed/update/[hash]', query: { hash } });

export const priceUpdateMethod = (tx: Pick<Transaction, 'price_update'>) => {
  const count = tx.price_update?.observation_count ?? 0;
  return `Price update · ${ count } market${ count === 1 ? '' : 's' }`;
};

interface Props {
  tx: Transaction;
  current?: string;
  isLoading?: boolean;
  mt?: number | string;
  fontWeight?: string;
}

export const PriceUpdateFromTo = ({ tx, current, isLoading, mt, fontWeight }: Props) => (
  <Flex alignItems="center" columnGap={ 3 } mt={ mt } fontWeight={ fontWeight } data-price-update-to>
    <AddressFromTo from={ tx.from } to={ null } current={ current } isLoading={ isLoading } mode="long"/>
    <Link href={ route({ pathname: '/feed' }) } loading={ isLoading } whiteSpace="nowrap">Price feed</Link>
  </Flex>
);
