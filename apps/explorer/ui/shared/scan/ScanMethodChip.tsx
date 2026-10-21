import React from 'react';

import { Tag } from 'toolkit/chakra/tag';

export interface ScanMethodChipProps {
  method: string;
  isLoading?: boolean;
  className?: string;
}

const ScanMethodChip = ({ method, isLoading, className }: ScanMethodChipProps) => {
  return (
    <Tag
      variant="outlined"
      loading={ isLoading }
      truncated
      className={ className }
      data-scan-method={ method }
    >
      { method }
    </Tag>
  );
};

export default React.memo(ScanMethodChip);
