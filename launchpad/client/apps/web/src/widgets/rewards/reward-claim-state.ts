import { decodeFunctionData, parseAbi } from 'viem';
import type { TransactionRequest } from '../../../../../packages/production-contracts/src/types.gen';
import { rawInteger, normalizeAddress } from '../profile/account-disclosure';
import type { Address, Hash, TransactionReceipt } from 'viem';
export interface ClaimIdentity { wallet: Address; chainId: 125; hash: Hash; nftId: bigint }
export type ClaimOutcome = 'pending' | 'account_changed' | 'confirmed' | 'reverted';
export function claimReceiptOutcome(identity: ClaimIdentity, wallet: string | undefined, receipt: TransactionReceipt | undefined): ClaimOutcome {
  if (!wallet || wallet.toLowerCase() !== identity.wallet.toLowerCase()) return 'account_changed';
  if (!receipt || receipt.transactionHash.toLowerCase() !== identity.hash.toLowerCase()) return 'pending';
  return receipt.status === 'success' ? 'confirmed' : 'reverted';
}

const feeClaimAbi = parseAbi(['function claimFees(uint256 nftId) returns (uint256 amount)']);
export function validatedRewardClaim(request: TransactionRequest, wallet: string, router: string) {
  if(request.chainId!=='125'||normalizeAddress(request.from)!==normalizeAddress(wallet)||normalizeAddress(request.to)!==normalizeAddress(router))throw new Error('Prepared claim authority mismatch');
  for(const value of [request.valueRaw,request.gasLimitRaw,request.maxFeePerGasRaw,request.maxPriorityFeePerGasRaw])if(value!==null&&rawInteger(value)>=(1n<<256n))throw new Error('Prepared claim amount exceeds uint256');
  if(rawInteger(request.valueRaw)!==0n)throw new Error('Fee claim cannot transfer native principal');
  const decoded=decodeFunctionData({abi:feeClaimAbi,data:request.data as `0x${string}`});
  if(decoded.functionName!=='claimFees')throw new Error('Unsupported reward settlement');
  return {nftId:decoded.args[0]};
}
