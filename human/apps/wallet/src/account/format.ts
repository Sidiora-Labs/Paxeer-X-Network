import { ethers } from 'ethers';
import type { CompletionAsset } from '@paxeer/wallet';
import { TOKENS } from '@/lib/constants';

export function units(amount: string, decimals: number | null | undefined): string {
    if (decimals === null || decimals === undefined) return amount;
    return ethers.formatUnits(BigInt(amount), decimals);
}

export function completionAssets(): CompletionAsset[] {
    const assets: CompletionAsset[] = [];
    for (const token of Object.values(TOKENS)) {
        if (token.native) {
            assets.push({ kind: 'native', symbol: token.symbol, decimals: token.decimals });
        } else if (token.address && ethers.isAddress(token.address)) {
            assets.push({ kind: 'erc20', address: token.address as `0x${string}`, symbol: token.symbol, decimals: token.decimals });
        }
    }
    return assets;
}
