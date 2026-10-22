import React from 'react';

import { Skeleton } from 'toolkit/chakra/skeleton';

import { formatPrice } from './utils';

interface Props {
  value: string;
  isLoading?: boolean;
}

const PriceValue = ({ value, isLoading }: Props) => (
  <Skeleton loading={ isLoading } display="inline-block" fontVariantNumeric="tabular-nums" data-price={ value }>
    { formatPrice(value) }
  </Skeleton>
);

export default PriceValue;
