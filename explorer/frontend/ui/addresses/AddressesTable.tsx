import type BigNumber from 'bignumber.js';
import React from 'react';

import type { AddressesItem } from 'types/api/addresses';

import { currencyUnits } from 'lib/units';
import { TableBody, TableColumnHeader, TableHeader, TableRoot, TableRow } from 'toolkit/chakra/table';
import { ZERO } from 'toolkit/utils/consts';

import AddressesTableItem from './AddressesTableItem';

interface Props {
  items: Array<AddressesItem>;
  totalSupply: BigNumber;
  pageStartIndex: number;
  top?: number;
  isLoading?: boolean;
}

const AddressesTable = ({ items, totalSupply, pageStartIndex, isLoading }: Props) => {
  const hasPercentage = !totalSupply.eq(ZERO);

  return (
    <TableRoot variant="scan" data-addresses-table>
      <TableHeader>
        <TableRow>
          <TableColumnHeader w="56px">#</TableColumnHeader>
          <TableColumnHeader w={ hasPercentage ? '32%' : '40%' }>Address</TableColumnHeader>
          <TableColumnHeader w={ hasPercentage ? '20%' : '22%' }>Name tag</TableColumnHeader>
          <TableColumnHeader w={ hasPercentage ? '22%' : '26%' } isNumeric>{ `Balance ${ currencyUnits.ether }` }</TableColumnHeader>
          { hasPercentage && <TableColumnHeader w="14%" isNumeric>Percentage</TableColumnHeader> }
          <TableColumnHeader w="12%" isNumeric>Txn count</TableColumnHeader>
        </TableRow>
      </TableHeader>
      <TableBody>
        { items.map((item, index) => (
          <AddressesTableItem
            key={ item.hash + (isLoading ? index : '') }
            item={ item }
            totalSupply={ totalSupply }
            index={ pageStartIndex + index }
            hasPercentage={ hasPercentage }
            isLoading={ isLoading }
          />
        )) }
      </TableBody>
    </TableRoot>
  );
};

export default AddressesTable;
