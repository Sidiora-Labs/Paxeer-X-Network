import type { ThemingConfig } from '@chakra-ui/react';

import type { ExcludeUndefined } from 'types/utils';

// The product radius scale: 4px marks, 8px chips and tiles, 12px inputs,
// 16px small cards, 24px cards and modals, 36px and 48px sheets.
export const radii: ExcludeUndefined<ThemingConfig['tokens']>['radii'] = {
  none: { value: '0' },
  xs: { value: '4px' },
  sm: { value: '8px' },
  base: { value: '12px' },
  md: { value: '16px' },
  lg: { value: '24px' },
  xl: { value: '36px' },
  '2xl': { value: '48px' },
  full: { value: '9999px' },
};
