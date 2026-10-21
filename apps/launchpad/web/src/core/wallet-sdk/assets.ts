import { measureWalletOperation } from './measurement';
import { erc20Abi, formatUnits, getAddress, keccak256, zeroAddress, type Address, type Hex } from 'viem';
import type { getPublicClient } from './client';

export type WalletPublicClient = ReturnType<typeof getPublicClient>;
export type AssetRef = Readonly<{ kind: 'native'; chainId: 125; symbol: 'PAX'; decimals: 18 }> | Readonly<{ kind: 'erc20'; chainId: 125; address: Address; symbol: 'USDL'; decimals: 6; runtimeHash?: Hex }>;
export const nativePAX: AssetRef = Object.freeze({ kind: 'native', chainId: 125, symbol: 'PAX', decimals: 18 });
/** Supply the approved current deployment registry address; symbol never proves identity. */
export function usdlAsset(address: string, runtimeHash?: Hex): AssetRef {
  const checked = getAddress(address);
  if (checked === zeroAddress) throw new Error('USDL requires a deployed token address');
  if (runtimeHash !== undefined && !/^0x[0-9a-fA-F]{64}$/.test(runtimeHash)) throw new Error('Invalid attested USDL runtime hash');
  return Object.freeze({ kind: 'erc20', chainId: 125, address: checked, symbol: 'USDL', decimals: 6, ...(runtimeHash === undefined ? {} : { runtimeHash }) });
}
export function assertAsset(asset: AssetRef) {
  if (asset.chainId !== 125 || (asset.kind === 'native' ? asset.decimals !== 18 || asset.symbol !== 'PAX' : asset.kind !== 'erc20' || asset.decimals !== 6 || asset.symbol !== 'USDL' || getAddress(asset.address) === zeroAddress)) throw new Error('Asset registry mismatch');
}
export function assetKey(asset: AssetRef) { assertAsset(asset); return asset.kind === 'native' ? '125:native' : `125:${getAddress(asset.address).toLowerCase()}`; }
const maxUint256 = (1n << 256n) - 1n;
export function rawAmount(raw: string): bigint {
  if (!/^(0|[1-9][0-9]{0,77})$/.test(raw)) throw new Error('Amount must be a canonical unsigned decimal integer');
  const amount = BigInt(raw);
  if (amount > maxUint256) throw new Error('Amount exceeds uint256');
  return amount;
}
export function parseAssetAmount(asset: AssetRef, input: string): bigint {
  assertAsset(asset);
  if (input.length > 100 || !/^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(input)) throw new Error('Invalid exact asset amount');
  const [whole, fraction = ''] = input.split('.');
  if (fraction.length > asset.decimals) throw new Error('Amount exceeds asset precision');
  return rawAmount(BigInt(whole! + fraction.padEnd(asset.decimals, '0')).toString());
}
export function formatAssetAmount(asset: AssetRef, raw: string) { assertAsset(asset); return `${formatUnits(rawAmount(raw), asset.decimals)} ${asset.symbol}`; }
export async function assertAssetOnChain(client: WalletPublicClient, asset: AssetRef, blockNumber?: bigint) {
  assertAsset(asset);
  if (await client.getChainId() !== 125) throw new Error('RPC requires chain 125');
  if (asset.kind === 'erc20') {
    const code = await client.getBytecode({ address: asset.address, blockNumber });
    if (!code || code === '0x') throw new Error('USDL contract code unavailable');
    if (asset.runtimeHash !== undefined && keccak256(code).toLowerCase() !== asset.runtimeHash.toLowerCase()) throw new Error('USDL runtime differs from the approved deployment; spending disabled');
    const decimals = await measureWalletOperation('asset.metadata', assetKey(asset), () => client.readContract({ address: asset.address, abi: erc20Abi, functionName: 'decimals', blockNumber }));
    if (decimals !== 6) throw new Error('USDL decimals mismatch; spending disabled');
  }
}
export async function readAssetBalance(client: WalletPublicClient, asset: AssetRef, account: Address, blockNumber: bigint) {
  await assertAssetOnChain(client, asset, blockNumber);
  return asset.kind === 'native' ? client.getBalance({ address: account, blockNumber }) : client.readContract({ address: asset.address, abi: erc20Abi, functionName: 'balanceOf', args: [account], blockNumber });
}
