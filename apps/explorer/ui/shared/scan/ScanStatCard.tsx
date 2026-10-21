import { chakra, Stat } from '@chakra-ui/react';
import React from 'react';

import { Skeleton } from 'toolkit/chakra/skeleton';
import { Hint } from 'toolkit/components/Hint/Hint';

export type ScanStatDeltaDirection = 'up' | 'down';

export interface ScanStatDelta {
  value: string;
  direction: ScanStatDeltaDirection;
}

export interface ScanStatCardProps {
  label: string;
  value: React.ReactNode;
  secondary?: React.ReactNode;
  delta?: ScanStatDelta;
  hint?: string;
  icon?: React.ReactNode;
  isLoading?: boolean;
  className?: string;
}

const ScanStatCard = ({ label, value, secondary, delta, hint, icon, isLoading, className }: ScanStatCardProps) => {
  return (
    <Stat.Root variant="scan" className={ className } data-scan-stat={ label }>
      <Stat.Label>
        { icon }
        <chakra.span data-label>{ label }</chakra.span>
        { hint && <Hint label={ hint } isLoading={ isLoading } boxSize={ 4 }/> }
      </Stat.Label>
      <Stat.ValueText>
        <Skeleton loading={ isLoading } data-value>{ value }</Skeleton>
        { secondary !== undefined && secondary !== null && (
          <chakra.span data-secondary>({ secondary })</chakra.span>
        ) }
        { delta && <chakra.span data-delta={ delta.direction }>({ delta.value })</chakra.span> }
      </Stat.ValueText>
    </Stat.Root>
  );
};

export default React.memo(ScanStatCard);
