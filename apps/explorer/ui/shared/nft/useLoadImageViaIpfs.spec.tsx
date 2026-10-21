// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { renderHook } from 'vitest/lib';

const { clientLoads, verifiedFetch } = vi.hoisted(() => ({
  clientLoads: { count: 0 },
  verifiedFetch: vi.fn(),
}));

vi.mock('@helia/verified-fetch', () => {
  clientLoads.count += 1;

  return { verifiedFetch };
});

import useLoadImageViaIpfs from './useLoadImageViaIpfs';

describe('useLoadImageViaIpfs', () => {
  beforeEach(() => {
    clientLoads.count = 0;
    verifiedFetch.mockReset();
    URL.createObjectURL = vi.fn(() => 'blob:image');
  });

  it('leaves the verified-fetch client alone until an image is loaded through it', () => {
    renderHook(() => useLoadImageViaIpfs());

    expect(clientLoads.count).toBe(0);
    expect(verifiedFetch).not.toHaveBeenCalled();
  });

  it('fetches the client on the first load and answers with the object url of the image', async() => {
    const blob = new Blob([ 'image' ]);
    verifiedFetch.mockResolvedValue({ status: 200, blob: () => Promise.resolve(blob) });

    const { result } = renderHook(() => useLoadImageViaIpfs());

    const src = await result.current('ipfs://bafyimage');

    expect(clientLoads.count).toBe(1);
    expect(verifiedFetch).toHaveBeenCalledWith('ipfs://bafyimage');
    expect(src).toBe('blob:image');
    expect(URL.createObjectURL).toHaveBeenCalledWith(blob);
  });

  it('fails when the gateway does not answer with the image', async() => {
    verifiedFetch.mockResolvedValue({ status: 404, blob: () => Promise.resolve(new Blob()) });

    const { result } = renderHook(() => useLoadImageViaIpfs());

    await expect(result.current('ipfs://bafymissing')).rejects.toThrow('Failed to load image');
  });
});
