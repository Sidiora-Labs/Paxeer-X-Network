import React from 'react';

import { Badge } from 'toolkit/chakra/badge';

export type ScanDirection = 'in' | 'out';

export interface ScanDirectionBadgeProps {
  direction: ScanDirection;
  isLoading?: boolean;
  className?: string;
}

const ScanDirectionBadge = ({ direction, isLoading, className }: ScanDirectionBadgeProps) => {
  return (
    <Badge
      variant="direction"
      colorPalette={ direction === 'in' ? 'green' : 'orange' }
      loading={ isLoading }
      className={ className }
      data-direction={ direction }
    >
      { direction === 'in' ? 'IN' : 'OUT' }
    </Badge>
  );
};

export default React.memo(ScanDirectionBadge);
