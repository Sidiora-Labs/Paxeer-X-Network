import { createListCollection, Flex, chakra } from '@chakra-ui/react';
import React from 'react';

import type { SelectOption } from 'toolkit/chakra/select';
import { Select } from 'toolkit/chakra/select';

export const SCAN_ROWS_PER_PAGE = [ 25, 50, 100 ];

export interface ScanShowRowsProps {
  value: number;
  onValueChange: (value: number) => void;
  options?: Array<number>;
  label?: string;
  suffix?: string;
  isLoading?: boolean;
  className?: string;
}

const ScanShowRows = ({
  value,
  onValueChange,
  options = SCAN_ROWS_PER_PAGE,
  label = 'Show rows:',
  suffix,
  isLoading,
  className,
}: ScanShowRowsProps) => {
  const collection = React.useMemo(() => createListCollection<SelectOption>({
    items: options.map((option) => ({ label: String(option), value: String(option) })),
  }), [ options ]);

  const handleValueChange = React.useCallback(({ value: next }: { value: Array<string> }) => {
    const parsed = Number(next[0]);

    if (Number.isFinite(parsed)) {
      onValueChange(parsed);
    }
  }, [ onValueChange ]);

  return (
    <Flex alignItems="center" columnGap={ 2 } className={ className } data-scan-show-rows>
      <chakra.span textStyle="sm" color="text.secondary" data-label>{ label }</chakra.span>
      <Select
        collection={ collection }
        placeholder={ label }
        value={ [ String(value) ] }
        onValueChange={ handleValueChange }
        loading={ isLoading }
        size="sm"
        w="fit-content"
        minW={ 20 }
        data-value={ String(value) }
      />
      { suffix && <chakra.span textStyle="sm" color="text.secondary" data-suffix>{ suffix }</chakra.span> }
    </Flex>
  );
};

export default React.memo(ScanShowRows);
