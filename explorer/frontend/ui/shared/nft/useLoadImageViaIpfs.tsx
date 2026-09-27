import React from 'react';

export default function useLoadImageViaIpfs() {
  return React.useCallback(async(url: string) => {
    // The verified-fetch client carries a full IPFS stack, which only the few pages that fall back to
    // it need, so it is fetched when the fallback is taken rather than with the page.
    const { verifiedFetch } = await import('@helia/verified-fetch');

    const response = await verifiedFetch(url);

    if (response.status !== 200) {
      throw new Error('Failed to load image');
    }

    const blob = await response.blob();
    const src = URL.createObjectURL(blob);
    return src;
  }, [ ]);
}
