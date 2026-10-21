import { Box } from '@chakra-ui/react';
import React from 'react';

import { IconButton } from 'toolkit/chakra/icon-button';
import { PopoverBody, PopoverContent, PopoverRoot, PopoverTrigger } from 'toolkit/chakra/popover';
import IconSvg from 'ui/shared/IconSvg';

export interface ScanPreviewButtonProps {
  label?: string;
  children: React.ReactNode;
  isLoading?: boolean;
  className?: string;
}

const ScanPreviewButton = ({ label = 'Preview', children, isLoading, className }: ScanPreviewButtonProps) => {
  return (
    <PopoverRoot>
      <PopoverTrigger>
        <IconButton
          aria-label={ label }
          variant="icon_secondary"
          size="2xs"
          borderRadius="sm"
          loadingSkeleton={ isLoading }
          className={ className }
          data-scan-preview
        >
          <IconSvg name="scope"/>
        </IconButton>
      </PopoverTrigger>
      <PopoverContent w="fit-content" maxW="100vw">
        <PopoverBody>
          <Box data-preview-body>{ children }</Box>
        </PopoverBody>
      </PopoverContent>
    </PopoverRoot>
  );
};

export default React.memo(ScanPreviewButton);
