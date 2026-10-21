import { Text } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import type { Block } from 'types/api/block';

import { currencyUnits } from 'lib/units';
import { Tooltip } from 'toolkit/chakra/tooltip';
import { ZERO } from 'toolkit/utils/consts';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import IconSvg from 'ui/shared/IconSvg';
import { ScanKeyValue } from 'ui/shared/scan';
import Utilization from 'ui/shared/Utilization/Utilization';
import GasPriceValue from 'ui/shared/value/GasPriceValue';
import NativeCoinValue from 'ui/shared/value/NativeCoinValue';

interface Props {
  data: Block;
}

const BlockDetailsBlobInfo = ({ data }: Props) => {
  if (
    !data.blob_gas_price ||
    !data.blob_gas_used ||
    !data.burnt_blob_fees ||
    !data.excess_blob_gas
  ) {
    return null;
  }

  const burntBlobFees = BigNumber(data.burnt_blob_fees || 0);
  const blobFees = BigNumber(data.blob_gas_price || 0).multipliedBy(BigNumber(data.blob_gas_used || 0));

  return (
    <>
      { data.blob_gas_price && (
        <ScanKeyValue
          label="Blob gas price"
          // eslint-disable-next-line max-len
          hint="Price per unit of gas used for for blob deployment. Blob gas is independent of normal gas. Both gas prices can affect the priority of transaction execution."
          multiRow
        >
          <GasPriceValue amount={ data.blob_gas_price }/>
        </ScanKeyValue>
      ) }
      { data.blob_gas_used && (
        <ScanKeyValue
          label="Blob gas used"
          hint="Actual amount of gas used by the blobs in this block"
        >
          <Text>{ BigNumber(data.blob_gas_used).toFormat() }</Text>
        </ScanKeyValue>
      ) }
      { !burntBlobFees.isEqualTo(ZERO) && (
        <ScanKeyValue
          label="Blob burnt fees"
          hint={ `Amount of ${ currencyUnits.ether } used for blobs in this block` }
          multiRow
        >
          <NativeCoinValue
            amount={ burntBlobFees.toString() }
            accuracy={ 0 }
            startElement={ <IconSvg name="flame" boxSize={ 5 } color="icon.primary" mr={{ base: 1, lg: 2 }}/> }
            mr={ 4 }
          />
          { !blobFees.isEqualTo(ZERO) && (
            <Tooltip content="Blob burnt fees / Txn fees * 100%">
              <Utilization value={ burntBlobFees.dividedBy(blobFees).toNumber() }/>
            </Tooltip>
          ) }
        </ScanKeyValue>
      ) }
      { data.excess_blob_gas && (
        <ScanKeyValue
          label="Excess blob gas"
          hint="A running total of blob gas consumed in excess of the target, prior to the block."
        >
          <GasPriceValue amount={ data.excess_blob_gas }/>
        </ScanKeyValue>
      ) }
      <DetailedInfo.ItemDivider data-scan-divider/>
    </>
  );
};

export default React.memo(BlockDetailsBlobInfo);
