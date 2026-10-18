import { useCallback, useRef } from "react";
import { useAccount, useWalletConnector } from "@paxeer-network/hooks";
import {
  AccountStatusEnum,
  ChainNamespace,
  SolanaChains,
  type WalletChainChangeState,
} from "@paxeer-network/types";
import { useWalletConnectValidation } from "./useWalletConnectValidation";

export type UseChainChangeValidationOptions = {
  onAccountValidated?: (status: AccountStatusEnum) => void;
};

export const useChainChangeValidation = (
  options: UseChainChangeValidationOptions = {},
) => {
  const { state } = useAccount();
  const { connectedChain, wallet } = useWalletConnector();
  const { waitForValidation, clearValidation } = useWalletConnectValidation();
  const requiresValidationRef = useRef(false);

  const onChainChangeBefore = useCallback(
    (chainId: number) => {
      clearValidation();

      const targetNamespace = SolanaChains.has(chainId)
        ? ChainNamespace.solana
        : ChainNamespace.evm;
      const walletAddress = wallet?.accounts[0]?.address;
      const shouldWaitForValidation =
        !!connectedChain &&
        (connectedChain.namespace !== targetNamespace ||
          (!!walletAddress && walletAddress !== state.address));

      requiresValidationRef.current = shouldWaitForValidation;
      if (shouldWaitForValidation) {
        waitForValidation((status) => {
          options.onAccountValidated?.(status);
        });
      }
    },
    [
      clearValidation,
      connectedChain,
      options.onAccountValidated,
      state.address,
      waitForValidation,
      wallet,
    ],
  );

  const onChainChangeAfter = useCallback(
    (_chainId: number, result: WalletChainChangeState) => {
      if (!result.isWalletConnected) {
        if (!result.isWalletConnectionPending) {
          requiresValidationRef.current = false;
          clearValidation();
        }
        return;
      }

      if (!requiresValidationRef.current) {
        options.onAccountValidated?.(state.status);
      }
    },
    [clearValidation, options.onAccountValidated, state.status],
  );

  return {
    onChainChangeBefore,
    onChainChangeAfter,
  } as const;
};
