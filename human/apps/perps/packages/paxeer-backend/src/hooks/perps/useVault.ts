import { VaultFactoryFacetABI as VaultFactoryFacetAbi } from "@paxeer-network/paxeer-backend/core/network/abi/VaultFactoryFacet";
import { TradingAccountABI as UserVaultAbi } from "@paxeer-network/paxeer-backend/core/network/abi/TradingAccount";
import { DIAMOND_ADDRESS, erc20Abi } from "@paxeer-network/paxeer-backend/core/perps/config";
import { readContract, writeContract, waitForTx } from "./useContract";

// ── Stable module-level functions (no React state, no hook needed) ──

export async function getVaultAddress(user: `0x${string}`): Promise<`0x${string}`> {
  return readContract({
    address: DIAMOND_ADDRESS,
    abi: VaultFactoryFacetAbi,
    functionName: "getVault",
    args: [user],
  }) as Promise<`0x${string}`>;
}

export async function createVault() {
  const hash = await writeContract({
    address: DIAMOND_ADDRESS,
    abi: VaultFactoryFacetAbi,
    functionName: "createVault",
  });
  return waitForTx(hash);
}

export async function getBalance(vaultAddress: `0x${string}`, token: `0x${string}`): Promise<bigint> {
  return readContract({
    address: vaultAddress,
    abi: UserVaultAbi,
    functionName: "getBalance",
    args: [token],
  }) as Promise<bigint>;
}

export async function getLockedBalance(vaultAddress: `0x${string}`, token: `0x${string}`): Promise<bigint> {
  return readContract({
    address: vaultAddress,
    abi: UserVaultAbi,
    functionName: "getLockedBalance",
    args: [token],
  }) as Promise<bigint>;
}

export async function deposit(vaultAddress: `0x${string}`, token: `0x${string}`, amount: bigint) {
  const hash = await writeContract({
    address: vaultAddress,
    abi: UserVaultAbi,
    functionName: "deposit",
    args: [token, amount],
  });
  return waitForTx(hash);
}

export async function withdraw(vaultAddress: `0x${string}`, token: `0x${string}`, amount: bigint) {
  const hash = await writeContract({
    address: vaultAddress,
    abi: UserVaultAbi,
    functionName: "withdraw",
    args: [token, amount],
  });
  return waitForTx(hash);
}

export async function approveToken(token: `0x${string}`, spender: `0x${string}`, amount: bigint) {
  const hash = await writeContract({
    address: token,
    abi: erc20Abi,
    functionName: "approve",
    args: [spender, amount],
  });
  return waitForTx(hash);
}

export async function getAllowance(token: `0x${string}`, owner: `0x${string}`, spender: `0x${string}`): Promise<bigint> {
  return readContract({
    address: token,
    abi: erc20Abi,
    functionName: "allowance",
    args: [owner, spender],
  }) as Promise<bigint>;
}

export async function getWalletBalance(token: `0x${string}`, user: `0x${string}`): Promise<bigint> {
  return readContract({
    address: token,
    abi: erc20Abi,
    functionName: "balanceOf",
    args: [user],
  }) as Promise<bigint>;
}

// Legacy hook wrapper — returns the same stable references
export function useVault() {
  return {
    getVaultAddress, createVault,
    getBalance, getLockedBalance,
    deposit, withdraw,
    approveToken, getAllowance, getWalletBalance,
  };
}
