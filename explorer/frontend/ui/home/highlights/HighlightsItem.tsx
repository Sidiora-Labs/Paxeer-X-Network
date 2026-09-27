import { Box } from '@chakra-ui/react';
import React from 'react';

import type { Route } from 'nextjs-routes';
import { route } from 'nextjs-routes';

import { Link } from 'toolkit/chakra/link';
import type { ScanStatDelta } from 'ui/shared/scan';
import { ScanStatCard } from 'ui/shared/scan';

export interface HighlightsItemProps {
  id: string;
  label: string;
  value: React.ReactNode;
  secondary?: React.ReactNode;
  delta?: ScanStatDelta;
  hint?: string;
  icon?: React.ReactNode;
  href?: Route;
  isLoading?: boolean;
}

const CARD_RESET = {
  '& [data-scan-stat]': {
    bg: 'transparent',
    borderWidth: '0',
    borderRadius: '0',
    boxShadow: 'none',
    px: '0',
    py: '0',
  },
};

const HighlightsItem = ({ id, label, value, secondary, delta, hint, icon, href, isLoading }: HighlightsItemProps) => {
  const card = (
    <ScanStatCard
      label={ label }
      value={ value }
      secondary={ secondary }
      delta={ delta }
      hint={ hint }
      icon={ icon }
      isLoading={ isLoading }
    />
  );

  return (
    <Box data-highlight={ id } px={{ base: 4, lg: 5 }} py={{ base: 3, lg: 4 }} css={ CARD_RESET }>
      { href && !isLoading ? (
        <Link href={ route(href) } variant="plain" display="block" w="100%">
          { card }
        </Link>
      ) : card }
    </Box>
  );
};

export default React.memo(HighlightsItem);
