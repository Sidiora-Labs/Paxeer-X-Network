import type { HTMLChakraProps } from '@chakra-ui/react';
import { chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import type { PaginationParams } from 'ui/shared/pagination/types';

import { Button } from 'toolkit/chakra/button';
import { IconButton } from 'toolkit/chakra/icon-button';
import IconSvg from 'ui/shared/IconSvg';

export interface ScanPaginationProps extends PaginationParams, Omit<HTMLChakraProps<'nav'>, 'page' | 'direction'> {
  pageCount?: number;
  onLastPageClick?: () => void;
}

const ScanPagination = (props: ScanPaginationProps) => {
  const {
    page,
    pageCount,
    onNextPageClick,
    onPrevPageClick,
    onLastPageClick,
    resetPage,
    hasNextPage,
    canGoBackwards,
    isLoading,
    isVisible,
    hasPages,
    ...rest
  } = props;

  if (!isVisible) {
    return null;
  }

  const isFirstPage = page === 1;

  return (
    <Flex as="nav" alignItems="center" columnGap={ 1 } data-scan-pagination { ...rest }>
      <Button
        variant="scan_control"
        size="sm"
        borderRadius="sm"
        onClick={ resetPage }
        disabled={ isFirstPage || isLoading }
        data-control="first"
      >
        First
      </Button>
      <IconButton
        aria-label="Previous page"
        variant="scan_control"
        borderRadius="sm"
        boxSize={ 8 }
        onClick={ onPrevPageClick }
        disabled={ !canGoBackwards || isLoading || isFirstPage }
        data-control="prev"
      >
        <IconSvg name="arrows/east-mini" boxSize={ 5 }/>
      </IconButton>
      <chakra.span
        textStyle="sm"
        color="text.secondary"
        borderWidth="1px"
        borderStyle="solid"
        borderColor="border.divider"
        borderRadius="sm"
        px={ 3 }
        py={ 1 }
        whiteSpace="nowrap"
        data-control="page"
      >
        { pageCount === undefined ? `Page ${ page }` : `Page ${ page } of ${ pageCount }` }
      </chakra.span>
      <IconButton
        aria-label="Next page"
        variant="scan_control"
        borderRadius="sm"
        boxSize={ 8 }
        onClick={ onNextPageClick }
        disabled={ !hasNextPage || isLoading }
        data-control="next"
      >
        <IconSvg name="arrows/east-mini" boxSize={ 5 } transform="rotate(180deg)"/>
      </IconButton>
      <Button
        variant="scan_control"
        size="sm"
        borderRadius="sm"
        onClick={ onLastPageClick }
        disabled={ !onLastPageClick || !hasNextPage || isLoading }
        data-control="last"
      >
        Last
      </Button>
    </Flex>
  );
};

export default React.memo(ScanPagination);
