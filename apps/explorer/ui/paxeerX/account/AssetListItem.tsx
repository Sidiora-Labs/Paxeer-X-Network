import { chakra, Flex, Grid } from '@chakra-ui/react';
import React from 'react';

import type { PaxeerXBalance } from 'types/api/paxeerX';

import { IconButton } from 'toolkit/chakra/icon-button';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableCell, TableRow } from 'toolkit/chakra/table';
import IconSvg from 'ui/shared/IconSvg';

import { assetLabel, formatAmount } from './utils';

const PART_LABELS: Array<{ key: keyof PaxeerXBalance['parts']; label: string }> = [
  { key: 'chain', label: 'On chain' },
  { key: 'custody', label: 'In custody' },
  { key: 'kernel', label: 'In kernel' },
];

export interface Props {
  item: PaxeerXBalance;
  isLoading?: boolean;
}

const AssetListItem = ({ item, isLoading }: Props) => {
  const [ isExpanded, setIsExpanded ] = React.useState(false);

  const handleToggle = React.useCallback(() => {
    setIsExpanded((prev) => !prev);
  }, []);

  const label = assetLabel(item.asset);

  return (
    <>
      <TableRow data-asset={ item.asset.id }>
        <TableCell verticalAlign="middle">
          <Flex columnGap={ 2 } alignItems="center" minW={ 0 }>
            <IconButton
              aria-label={ isExpanded ? `Hide ${ label } breakdown` : `Show ${ label } breakdown` }
              aria-expanded={ isExpanded }
              onClick={ handleToggle }
              disabled={ isLoading }
              variant="icon_secondary"
              size="2xs"
              boxSize={ 5 }
              flexShrink={ 0 }
            >
              <IconSvg name="arrows/east-mini" transform={ isExpanded ? 'rotate(90deg)' : undefined }/>
            </IconButton>
            <Flex flexDirection="column" rowGap={ 1 } minW={ 0 }>
              <Skeleton loading={ isLoading } fontWeight={ 600 }>{ label }</Skeleton>
              { item.asset.id !== label && (
                <Skeleton loading={ isLoading } color="text.secondary" textStyle="xs" wordBreak="break-all" data-label="asset-id">
                  { item.asset.id }
                </Skeleton>
              ) }
            </Flex>
          </Flex>
        </TableCell>
        <TableCell verticalAlign="middle">
          <Skeleton loading={ isLoading } color="text.secondary" wordBreak="break-all">{ item.asset.denom }</Skeleton>
        </TableCell>
        <TableCell verticalAlign="middle" isNumeric>
          <Skeleton loading={ isLoading } display="inline-block" fontWeight={ 500 } data-label="total">
            { formatAmount(item.total, item.asset) }
          </Skeleton>
        </TableCell>
      </TableRow>
      { isExpanded && (
        <TableRow data-parts-of={ item.asset.id }>
          <TableCell colSpan={ 3 } pt={ 0 }>
            <Grid
              templateColumns={{ base: 'minmax(0, 1fr)', lg: 'repeat(3, minmax(0, 1fr))' }}
              gap={ 3 }
              bg="bg.sunken"
              borderRadius="sm"
              px={ 4 }
              py={ 3 }
            >
              { PART_LABELS.map(({ key, label: partLabel }) => (
                <Flex key={ key } flexDirection="column" rowGap={ 1 } minW={ 0 }>
                  <chakra.span textStyle="xs" color="text.secondary">{ partLabel }</chakra.span>
                  <chakra.span textStyle="sm" color="text.primary" wordBreak="break-all" data-part={ key }>
                    { formatAmount(item.parts[key], item.asset) }
                  </chakra.span>
                </Flex>
              )) }
            </Grid>
          </TableCell>
        </TableRow>
      ) }
    </>
  );
};

export default React.memo(AssetListItem);
