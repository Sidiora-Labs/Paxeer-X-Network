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

// The card body holds either a desktop table, which keeps its own edge-to-edge rules and scrolls
// inside the body, or the mobile list items, which carry no gutter of their own and would otherwise
// sit flush against the card border on a narrow screen.
const BODY_GUTTER = {
  '& [data-list-item-mobile]': {
    px: '4',
  },
};

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
        minW={ 0 }
      >
        <Box minW={ 0 }>
          <chakra.p textStyle="sm" fontWeight="500" color="text.primary" data-title>{ title }</chakra.p>
          { note && <chakra.p textStyle="xs" color="text.muted" data-note>{ note }</chakra.p> }
        </Box>
        <Flex alignItems="center" flexWrap="wrap" columnGap={ 2 } rowGap={ 2 } minW={ 0 } data-actions>
          { actions }
          { pagination && (
            <Box hideBelow="md" data-header-pagination>{ pagination }</Box>
          ) }
        </Flex>
      </Flex>
      <Box data-body overflowX="auto" maxW="100%" css={ BODY_GUTTER }>
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
          <Box minW={ 0 } data-footer-rows>{ showRows }</Box>
          <Box minW={ 0 } data-footer-pagination>{ pagination }</Box>
        </Flex>
      ) }
    </Box>
  );
};

export default React.memo(ScanTableCard);
