'use client'

import { installPostMessageEip1193Provider } from './post-message-eip1193'

/**
 * Side-effect module: installs the postMessage EIP-1193 bridge
 * if we're running inside an iframe. Must be imported BEFORE wagmi config
 * so that `injected()` picks up our provider as `window.ethereum`.
 *
 * Import this at the top of app-providers.tsx.
 */
if (typeof window !== 'undefined' && window.parent !== window) {
  installPostMessageEip1193Provider()
}
