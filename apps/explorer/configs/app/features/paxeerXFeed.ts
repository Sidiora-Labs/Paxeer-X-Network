import type { Feature } from './types';

import { getEnvValue } from '../utils';

// The feed pages ship with the Paxeer X build; whether the node indexes price updates is read at
// runtime from the `price_feed` flag of /api/v2/paxeer-x/capabilities.
const title = 'Price feed';

const config: Feature<{ isEnabled: true }> = (() => {
  if (getEnvValue('NEXT_PUBLIC_PAXEER_X_ENABLED') === 'true') {
    return Object.freeze({
      title,
      isEnabled: true,
    });
  }

  return Object.freeze({
    title,
    isEnabled: false,
  });
})();

export default config;
