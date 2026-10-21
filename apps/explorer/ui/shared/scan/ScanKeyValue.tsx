import React from 'react';

import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';

export interface ScanKeyValueProps {
  label: React.ReactNode;
  hint?: React.ReactNode;
  children: React.ReactNode;
  id?: string;
  isLoading?: boolean;
  multiRow?: boolean;
  withDivider?: boolean;
  className?: string;
}

const ScanKeyValue = ({ label, hint, children, id, isLoading, multiRow, withDivider, className }: ScanKeyValueProps) => {
  return (
    <>
      <DetailedInfo.ItemLabel
        hint={ hint }
        isLoading={ isLoading }
        id={ id }
        className={ className }
        data-scan-key
      >
        { label }
      </DetailedInfo.ItemLabel>
      <DetailedInfo.ItemValue multiRow={ multiRow } data-scan-value>
        { children }
      </DetailedInfo.ItemValue>
      { withDivider && <DetailedInfo.ItemDivider data-scan-divider/> }
    </>
  );
};

export default React.memo(ScanKeyValue);
