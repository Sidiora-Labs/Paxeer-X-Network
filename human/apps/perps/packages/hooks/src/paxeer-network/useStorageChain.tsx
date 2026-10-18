import { ChainKey, ChainNamespace, SolanaChains } from "@paxeer-network/types";
import { useLocalStorage } from "../useLocalStorage";

export function useStorageChain() {
  const [chain, setChain] = useLocalStorage<{chainId:number, namespace: ChainNamespace} | null>(ChainKey,null);
  const setStorageChain = (chainId: number) => {
    let namespace: ChainNamespace = ChainNamespace.evm;
    if (SolanaChains.has(chainId)) {
      namespace = ChainNamespace.solana;
    }

    setChain({
      chainId: chainId,
      namespace,
    })

  }

  return {
    storageChain: chain,
    setStorageChain,
  }
}