/**
 * EIP-1193 bridge for the Paxeer Embedded Wallet.
 *
 * The new `@paxeer/wallet` SDK is a REST client over `connect.paxportwallet.com`,
 * not a wallet provider. wagmi v2 connectors expect an EIP-1193 provider, so
 * this module synthesizes one:
 *
 *   wallet methods   (eth_sendTransaction, personal_sign, …)
 *     → forwarded to the SDK (server signs + broadcasts)
 *
 *   read methods     (eth_call, eth_getBalance, eth_blockNumber, …)
 *     → forwarded to the public Paxeer RPC over fetch
 *
 *   chain management (eth_chainId, wallet_switchEthereumChain)
 *     → handled locally; we are single-chain (125) for now
 *
 *   auth state changes
 *     → emitted as `accountsChanged` / `connect` / `disconnect` events
 *
 * Supabase OAuth flows redirect away from the page, so the bridge must
 * survive a full reload — every method re-reads the session from the SDK.
 */

import type { PaxeerWallet } from "./sdk";
import type { Session } from "@supabase/supabase-js";
import { getAddress as toChecksum } from "viem";

type RequestArgs = { method: string; params?: unknown[] | Record<string, unknown> };
type Listener = (...args: unknown[]) => void;

export type EIP1193Event =
  | "connect"
  | "disconnect"
  | "accountsChanged"
  | "chainChanged"
  | "message";

export interface PaxeerEIP1193Provider {
  readonly isPaxeerEmbedded: true;
  request(args: RequestArgs): Promise<unknown>;
  on(event: string, listener: Listener): void;
  removeListener(event: string, listener: Listener): void;
  /** Tear down listeners + auth subscription. Called by the connector on disconnect. */
  destroy(): void;
}

export interface PaxeerProviderOptions {
  wallet: PaxeerWallet;
  chainId: number;
  rpcUrl: string;
  fetchImpl?: typeof fetch;
}

export function createPaxeerEIP1193Provider(opts: PaxeerProviderOptions): PaxeerEIP1193Provider {
  const { wallet, chainId, rpcUrl } = opts;
  const fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis);
  const chainIdHex = `0x${chainId.toString(16)}`;

  const listeners = new Map<EIP1193Event, Set<Listener>>();
  let cachedAddress: `0x${string}` | null = null;
  let rpcId = 0;

  function emit(event: EIP1193Event, ...args: unknown[]): void {
    const set = listeners.get(event);
    if (!set) return;
    for (const listener of set) {
      try {
        listener(...args);
      } catch {
        /* swallow — wagmi listeners must not break our auth flow */
      }
    }
  }

  async function getAddress(): Promise<`0x${string}` | null> {
    const session = await wallet.getSession();
    if (!session) {
      cachedAddress = null;
      return null;
    }
    if (cachedAddress) return cachedAddress;
    try {
      const r = await wallet.getWallet();
      // The wallet API stores addresses lowercase. wagmi v2's `useAccount`
      // surfaces whatever the connector returns without normalization, so a
      // lowercase address here means every downstream consumer that keys on
      // address (indexers, subgraph queries, log filters, address-equality
      // checks) sees a different string than injected wallets, which return
      // EIP-55 checksummed. Normalize here so both connectors are
      // indistinguishable to the rest of the app.
      cachedAddress = toChecksum(r.wallet.address);
      return cachedAddress;
    } catch {
      return null;
    }
  }

  /**
   * viem/wagmi serialize transaction quantity fields as hex (`0x2dc6c0`).
   * The SDK's `TxRequest` expects either a decimal string or a bigint
   * ("Decimal string in wei"). Sending hex straight through gets rejected
   * by `connect.paxportwallet.com` with a 400. Normalize every quantity
   * field to bigint before forwarding.
   */
  function toBigIntOrUndef(v: unknown): bigint | undefined {
    if (v === undefined || v === null || v === "") return undefined;
    if (typeof v === "bigint") return v;
    if (typeof v === "number") return BigInt(v);
    if (typeof v === "string") {
      // BigInt() accepts both "0x.." and decimal strings.
      try {
        return BigInt(v);
      } catch {
        return undefined;
      }
    }
    return undefined;
  }

  function toIntOrUndef(v: unknown): number | undefined {
    if (v === undefined || v === null || v === "") return undefined;
    if (typeof v === "number") return v;
    if (typeof v === "bigint") return Number(v);
    if (typeof v === "string") {
      try {
        return Number(BigInt(v));
      } catch {
        return undefined;
      }
    }
    return undefined;
  }

  async function rpcCall(method: string, params: unknown): Promise<unknown> {
    const id = ++rpcId;
    const res = await fetchImpl(rpcUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ jsonrpc: "2.0", id, method, params: params ?? [] }),
    });
    const json = (await res.json()) as { result?: unknown; error?: { code: number; message: string } };
    if (json.error) {
      const err = new Error(json.error.message) as Error & { code?: number };
      err.code = json.error.code;
      throw err;
    }
    return json.result;
  }

  function asArray(params: RequestArgs["params"]): unknown[] {
    return Array.isArray(params) ? params : params ? [params] : [];
  }

  function decodeMessage(raw: unknown): string {
    if (typeof raw !== "string") return String(raw ?? "");
    if (raw.startsWith("0x") && /^0x[0-9a-fA-F]*$/.test(raw)) {
      try {
        const bytes = raw.slice(2);
        const buf = new Uint8Array(bytes.length / 2);
        for (let i = 0; i < buf.length; i++) {
          buf[i] = parseInt(bytes.slice(i * 2, i * 2 + 2), 16);
        }
        return new TextDecoder("utf-8", { fatal: false }).decode(buf);
      } catch {
        return raw;
      }
    }
    return raw;
  }

  // Track auth changes so wagmi gets accountsChanged / connect / disconnect.
  const unsubAuth = wallet.onAuthStateChange((event, session: Session | null) => {
    void (async () => {
      if (!session) {
        const had = cachedAddress;
        cachedAddress = null;
        if (had) {
          emit("accountsChanged", []);
          emit("disconnect", { code: 4900, message: "user signed out" });
        }
        return;
      }
      // SIGNED_IN, TOKEN_REFRESHED, INITIAL_SESSION → make sure we have an address.
      const before = cachedAddress;
      const addr = await getAddress();
      if (addr && addr !== before) {
        emit("accountsChanged", [addr]);
        if (!before) {
          emit("connect", { chainId: chainIdHex });
        }
      }
      void event; // event unused, but kept in signature for clarity
    })();
  });

  return {
    isPaxeerEmbedded: true as const,

    async request({ method, params }: RequestArgs): Promise<unknown> {
      const args = asArray(params);

      switch (method) {
        case "eth_chainId":
        case "net_version":
          return method === "net_version" ? String(chainId) : chainIdHex;

        case "eth_accounts": {
          const addr = await getAddress();
          return addr ? [addr] : [];
        }

        case "eth_requestAccounts": {
          const addr = await getAddress();
          if (!addr) {
            const err = new Error(
              "User is not signed in to the Paxeer Embedded Wallet. Open the connect modal first.",
            ) as Error & { code?: number };
            err.code = 4100;
            throw err;
          }
          return [addr];
        }

        case "eth_sendTransaction": {
          const tx = (args[0] ?? {}) as Record<string, unknown>;
          const r = await wallet.sendTransaction({
            to: tx.to as `0x${string}` | undefined,
            value: toBigIntOrUndef(tx.value),
            data: tx.data as `0x${string}` | undefined,
            gas: toBigIntOrUndef(tx.gas ?? tx.gasLimit),
            maxFeePerGas: toBigIntOrUndef(tx.maxFeePerGas),
            maxPriorityFeePerGas: toBigIntOrUndef(tx.maxPriorityFeePerGas),
            nonce: toIntOrUndef(tx.nonce),
            chainId,
          });
          return r.tx_hash;
        }

        case "eth_signTransaction": {
          const tx = (args[0] ?? {}) as Record<string, unknown>;
          const r = await wallet.signTransaction({
            to: tx.to as `0x${string}` | undefined,
            value: toBigIntOrUndef(tx.value),
            data: tx.data as `0x${string}` | undefined,
            gas: toBigIntOrUndef(tx.gas ?? tx.gasLimit),
            maxFeePerGas: toBigIntOrUndef(tx.maxFeePerGas),
            maxPriorityFeePerGas: toBigIntOrUndef(tx.maxPriorityFeePerGas),
            nonce: toIntOrUndef(tx.nonce),
            chainId,
          });
          return r.signed_tx;
        }

        case "personal_sign": {
          // params: [message, address]
          const message = decodeMessage(args[0]);
          const r = await wallet.signMessage(message);
          return r.signature;
        }

        case "eth_sign": {
          // params: [address, message]  (legacy order; behave identically to personal_sign)
          const message = decodeMessage(args[1]);
          const r = await wallet.signMessage(message);
          return r.signature;
        }

        case "eth_signTypedData_v4":
        case "eth_signTypedData_v3":
        case "eth_signTypedData": {
          const err = new Error(
            "eth_signTypedData_v4 is not yet supported by the Paxeer Embedded Wallet API. Use an injected wallet for EIP-712 signing for now.",
          ) as Error & { code?: number };
          err.code = 4200; // Unsupported Method
          throw err;
        }

        case "wallet_switchEthereumChain": {
          const target = (args[0] as { chainId?: string } | undefined)?.chainId;
          if (target && target.toLowerCase() === chainIdHex) return null;
          const err = new Error(
            `Chain ${target ?? "?"} is not supported by the Paxeer Embedded Wallet (only ${chainIdHex} / 125).`,
          ) as Error & { code?: number };
          err.code = 4902; // Chain Not Added
          throw err;
        }

        case "wallet_addEthereumChain":
          // Single-chain wallet: nothing to add, succeed silently.
          return null;

        case "wallet_getPermissions":
        case "wallet_requestPermissions": {
          const addr = await getAddress();
          return addr
            ? [{ parentCapability: "eth_accounts", caveats: [{ type: "restrictReturnedAccounts", value: [addr] }] }]
            : [];
        }

        // Read methods → forward to public RPC.
        default:
          return rpcCall(method, args);
      }
    },

    on(event: string, listener: Listener): void {
      const key = event as EIP1193Event;
      const set = listeners.get(key) ?? new Set<Listener>();
      set.add(listener);
      listeners.set(key, set);
    },

    removeListener(event: string, listener: Listener): void {
      listeners.get(event as EIP1193Event)?.delete(listener);
    },

    destroy(): void {
      unsubAuth();
      listeners.clear();
      cachedAddress = null;
    },
  };
}
