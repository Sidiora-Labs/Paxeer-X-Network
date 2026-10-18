import { useMemo } from "react";
import { useChains, useConfig } from "@paxeer-network/hooks";
import { useTokensInfo } from "@paxeer-network/hooks";
import { NetworkId } from "@paxeer-network/types";

export type ExclusiveDepositNetwork = {
  label: string;
  value: string;
  chainId: number;
  explorerUrl: string;
};

export type ExclusiveDepositToken = {
  label: string;
  value: string;
};

export const useExclusiveDepositOptions = (params?: {
  selectedNetwork?: string;
}) => {
  const networkId = useConfig("networkId") as NetworkId;
  const tokensInfo = useTokensInfo(networkId);
  const selectedNetwork = params?.selectedNetwork;

  const [, { findByChainId }] = useChains(networkId, {
    pick: "network_infos",
  });

  const combos = useMemo(() => {
    if (!tokensInfo?.length) return [];

    const result: {
      chainId: number;
      chainName: string;
      explorerUrl: string;
      tokenSymbol: string;
      displayName: string;
    }[] = [];

    for (const token of tokensInfo) {
      for (const detail of token.chain_details ?? []) {
        if (detail.exclusive_deposit_supported === true) {
          const chainId = Number(detail.chain_id);
          const chain = findByChainId(chainId);
          result.push({
            chainId,
            chainName:
              (chain as any)?.network_infos?.name ?? `Chain ${chainId}`,
            explorerUrl: (chain as any)?.network_infos?.explorer_base_url ?? "",
            tokenSymbol: token.token,
            displayName: detail.display_name || token.token,
          });
        }
      }
    }

    return result;
  }, [tokensInfo, findByChainId]);

  const networkOptions = useMemo<ExclusiveDepositNetwork[]>(() => {
    const seen = new Map<number, ExclusiveDepositNetwork>();

    for (const combo of combos) {
      if (!seen.has(combo.chainId)) {
        seen.set(combo.chainId, {
          label: combo.chainName,
          value: String(combo.chainId),
          chainId: combo.chainId,
          explorerUrl: combo.explorerUrl,
        });
      }
    }

    const list = Array.from(seen.values());
    // Arbitrum (42161) first for backward compatibility
    list.sort((a, b) => {
      if (a.chainId === 42161) return -1;
      if (b.chainId === 42161) return 1;
      return 0;
    });

    return list;
  }, [combos]);

  const tokenOptions = useMemo<ExclusiveDepositToken[]>(() => {
    if (!selectedNetwork) return [];

    const chainId = Number(selectedNetwork);
    const seen = new Set<string>();
    const result: ExclusiveDepositToken[] = [];

    for (const combo of combos) {
      if (combo.chainId === chainId && !seen.has(combo.tokenSymbol)) {
        seen.add(combo.tokenSymbol);
        result.push({ label: combo.displayName, value: combo.tokenSymbol });
      }
    }

    // USDC first for backward compatibility
    result.sort((a, b) => {
      if (a.value === "USDC") return -1;
      if (b.value === "USDC") return 1;
      return 0;
    });

    return result;
  }, [combos, selectedNetwork]);

  const isSupported = combos.length > 0;

  return { networkOptions, tokenOptions, isSupported };
};
