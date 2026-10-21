/**
 * Perps wallet hook — wagmi-backed.
 *
 * Reads connection state from wagmi (useAccount, useDisconnect)
 * and syncs it into the Zustand wallet store for backward compat.
 * Injected wallets (Paxport, MetaMask, Rabby, etc.) handle signing.
 *
 * `isConnected` is stabilized across reconnect cycles so the UI does not
 * flash "Connect Wallet" while wagmi is auto-reconnecting on page load.
 * We cache a `hasConnectedBefore` hint in localStorage — when it is set,
 * we treat the wallet as connected until wagmi explicitly reports
 * disconnected (i.e. not while still connecting or reconnecting).
 */
"use client";

import { useEffect, useMemo, useCallback, useRef, useState } from "react";
import { useAccount, useDisconnect, useChainId, useSwitchChain } from "wagmi";
import { useWalletStore } from "@/hooks/wallet/useWalletStore";
import { paxeerChain } from "@/core/chain";
import { PAXEER_CONNECTOR_ID } from "@/core/wallet/embedded";

const CONNECTED_HINT_KEY = "paxeer:wallet:connected-before";

function readConnectedHint(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(CONNECTED_HINT_KEY) === "1";
  } catch {
    return false;
  }
}

function writeConnectedHint(value: boolean) {
  if (typeof window === "undefined") return;
  try {
    if (value) window.localStorage.setItem(CONNECTED_HINT_KEY, "1");
    else window.localStorage.removeItem(CONNECTED_HINT_KEY);
  } catch {
    /* ignore quota / privacy errors */
  }
}

export function usePerpsWallet() {
  const { address, isConnected, isConnecting, isReconnecting, status: wagmiStatus, connector } = useAccount();
  const { disconnect: wagmiDisconnect } = useDisconnect();
  const chainId = useChainId();
  const { switchChain } = useSwitchChain();

  const setConnected = useWalletStore((s) => s.setConnected);
  const setDisconnected = useWalletStore((s) => s.setDisconnected);
  const openConnectModal = useWalletStore((s) => s.openConnectModal);

  // Track whether we've ever been connected this session to avoid
  // clobbering state during wagmi's async reconnect window on page load
  const hasBeenConnected = useRef(false);

  // Hydrate-safe connected hint. Starts false on SSR/first render so React
  // sees the same tree on server and client; we flip to the real value in
  // a useEffect. That means we only ever *extend* the connected state —
  // never flip it off from what wagmi reports.
  const [hasConnectedHint, setHasConnectedHint] = useState(false);
  useEffect(() => {
    setHasConnectedHint(readConnectedHint());
  }, []);

  // Sync wagmi state → Zustand store + localStorage hint
  useEffect(() => {
    if (isConnected && address) {
      hasBeenConnected.current = true;
      writeConnectedHint(true);
      setConnected(address, chainId);
    } else if (!isConnected && !isConnecting && !isReconnecting && wagmiStatus === "disconnected") {
      // Only set disconnected if wagmi has fully settled AND we're not
      // in the middle of a reconnect cycle (which happens on every page load)
      if (hasBeenConnected.current) {
        setDisconnected();
      }
    }
  }, [isConnected, isConnecting, isReconnecting, wagmiStatus, address, chainId, setConnected, setDisconnected]);

  const isCorrectChain = chainId === paxeerChain.id;

  const shortAddress = useMemo(() => {
    if (!address) return "";
    return `${address.slice(0, 6)}...${address.slice(-4)}`;
  }, [address]);

  const connect = useCallback(() => {
    openConnectModal();
  }, [openConnectModal]);

  const disconnect = useCallback(() => {
    wagmiDisconnect();
    hasBeenConnected.current = false;
    writeConnectedHint(false);
    setHasConnectedHint(false);
    setDisconnected();
  }, [wagmiDisconnect, setDisconnected]);

  const switchToPaxeer = useCallback(() => {
    if (switchChain) {
      switchChain({ chainId: paxeerChain.id });
    }
  }, [switchChain]);

  // Stabilized flag: true while wagmi is actively connected OR while it's
  // still attempting an auto-reconnect for a user we know was connected
  // before. This prevents the "Connect Wallet" flash on page load.
  //
  // wagmi's `status` is a discriminated union keyed on `isConnected /
  // isConnecting / isReconnecting`, so explicitly comparing `wagmiStatus`
  // against "connecting"/"reconnecting" is redundant and causes TS2367
  // ("no overlap") in the narrowed branch. `isConnecting || isReconnecting`
  // already covers both cases.
  const stableIsConnected =
    isConnected ||
    ((isConnecting || isReconnecting) &&
      (hasConnectedHint || hasBeenConnected.current));

  // F-12: capability flags.
  // The Paxeer Embedded Wallet's REST API doesn't yet support
  // `eth_signTypedData_v4` (returns EIP-1193 4200 "Unsupported Method").
  // Anything that depends on EIP-712 (RFQ off-chain quotes, gasless
  // session keys, Permit2 approvals) MUST be gated behind
  // `canSignTypedData` — either hide the affordance, or steer the user
  // to an injected wallet, or fall back to an on-chain alternative.
  const isEmbeddedWallet = connector?.id === PAXEER_CONNECTOR_ID;
  const canSignTypedData = isConnected && !isEmbeddedWallet;

  return {
    address: address ?? null,
    isConnected: stableIsConnected,
    isConnecting: isConnecting || isReconnecting,
    isLocked: false,
    chainId: isConnected ? chainId : null,
    error: null,
    shortAddress,
    isCorrectChain,
    isEmbeddedWallet,
    canSignTypedData,
    connect,
    disconnect,
    switchToPaxeer,
  };
}
