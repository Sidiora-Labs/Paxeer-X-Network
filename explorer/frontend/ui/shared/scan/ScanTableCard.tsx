import { Box, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

export type ScanTableCountKind = 'more_than' | 'total' | 'latest';

export interface ScanTableCount {
  kind: ScanTableCountKind;
  value: number;
  itemsName: string;
  shownValue?: number;
}

export interface ScanTableCardProps {
  title: React.ReactNode;
  note?: React.ReactNode;
  actions?: React.ReactNode;
  pagination?: React.ReactNode;
  showRows?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

export function formatScanTableCount({ kind, value, itemsName, shownValue }: ScanTableCount): string {
  switch (kind) {
    case 'total':
      return `A total of ${ value.toLocaleString() } ${ itemsName } found`;
    case 'latest':
      return `Latest ${ (shownValue ?? value).toLocaleString() } from a total of ${ value.toLocaleString() } ${ itemsName }`;
    case 'more_than':
      return `More than ${ value.toLocaleString() } ${ itemsName } found`;
  }
}

const ScanTableCard = ({ title, note, actions, pagination, showRows, children, className }: ScanTableCardProps) => {
  const hasFooter = Boolean(showRows || pagination);

  return (
    <Box
      className={ className }
      data-scan-table-card
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      overflow="hidden"
    >
      <Flex
        data-header
        alignItems="flex-start"
        justifyContent="space-between"
        flexWrap="wrap"
        columnGap={ 3 }
        rowGap={ 3 }
        px={ 4 }
        py={ 3 }
      >
        <Box minW={ 0 }>
          <chakra.p textStyle="sm" fontWeight="500" color="text.primary" data-title>{ title }</chakra.p>
          { note && <chakra.p textStyle="xs" color="text.muted" data-note>{ note }</chakra.p> }
        </Box>
        <Flex alignItems="center" flexWrap="wrap" columnGap={ 2 } rowGap={ 2 } data-actions>
          { actions }
          { pagination }
        </Flex>
      </Flex>
      <Box data-body overflowX="auto">
        { children }
      </Box>
      { hasFooter && (
        <Flex
          data-footer
          alignItems="center"
          justifyContent="space-between"
          flexWrap="wrap"
          columnGap={ 3 }
          rowGap={ 3 }
          px={ 4 }
          py={ 3 }
          borderTopWidth="1px"
          borderStyle="solid"
          borderColor="border.divider"
        >
          <Box data-footer-rows>{ showRows }</Box>
          <Box data-footer-pagination>{ pagination }</Box>
        </Flex>
      ) }
    </Box>
  );
};

export default React.memo(ScanTableCard);
