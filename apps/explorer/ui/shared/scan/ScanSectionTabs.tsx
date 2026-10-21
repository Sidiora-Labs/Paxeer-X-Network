import { chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import { TabsList, TabsRoot, TabsTrigger } from 'toolkit/chakra/tabs';

export interface ScanSectionTabItem {
  id: string;
  title: string;
  count?: number | null;
  disabled?: boolean;
}

export interface ScanSectionTabsProps {
  items: Array<ScanSectionTabItem>;
  value: string;
  onValueChange: (value: string) => void;
  rightSlot?: React.ReactNode;
  className?: string;
}

const ScanSectionTabs = ({ items, value, onValueChange, rightSlot, className }: ScanSectionTabsProps) => {
  const handleValueChange = React.useCallback(({ value: next }: { value: string }) => {
    onValueChange(next);
  }, [ onValueChange ]);

  return (
    <Flex
      className={ className }
      data-scan-section-tabs
      alignItems="center"
      justifyContent="space-between"
      columnGap={ 3 }
      rowGap={ 3 }
      flexWrap="wrap"
      w="100%"
    >
      <TabsRoot variant="pill" size="sm" value={ value } onValueChange={ handleValueChange } w="auto" maxW="100%" minW={ 0 }>
        <TabsList>
          { items.map((item) => (
            <TabsTrigger key={ item.id } value={ item.id } disabled={ item.disabled } data-tab={ item.id } maxW="100%" minW={ 0 }>
              <chakra.span data-tab-title overflow="hidden" textOverflow="ellipsis" whiteSpace="nowrap" minW={ 0 }>
                { item.title }
              </chakra.span>
              { item.count !== undefined && item.count !== null && (
                <chakra.span data-count flexShrink={ 0 }>({ item.count.toLocaleString() })</chakra.span>
              ) }
            </TabsTrigger>
          )) }
        </TabsList>
      </TabsRoot>
      { rightSlot && <Flex alignItems="center" columnGap={ 2 } data-right-slot>{ rightSlot }</Flex> }
    </Flex>
  );
};

export default React.memo(ScanSectionTabs);
