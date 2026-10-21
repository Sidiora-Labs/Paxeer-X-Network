import type { EvmAddress, EvmSignature } from "../../api/index.ts";
import type { Eip1193Provider } from "../../journeys/custody/handoff.ts";

export type WalletBridgeFailure = "cancelled" | "rejected" | "unavailable" | "failed";

export type WalletAccountOutcome =
  | Readonly<{ outcome: "approved"; address: EvmAddress }>
  | Readonly<{ outcome: WalletBridgeFailure }>;

export type WalletSignatureOutcome =
  | Readonly<{ outcome: "approved"; signature: EvmSignature }>
  | Readonly<{ outcome: WalletBridgeFailure }>;

export interface BindingWalletBridge {
  account(): Promise<WalletAccountOutcome>;
  sign(address: EvmAddress, statement: string): Promise<WalletSignatureOutcome>;
}

const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/u;
const EVM_SIGNATURE = /^0x[0-9a-fA-F]{130}$/u;
const USER_REJECTED_REQUEST = 4001;
const UNAUTHORIZED_REQUEST = 4100;
const PROVIDER_DISCONNECTED = 4900;
const CHAIN_DISCONNECTED = 4901;

function failure(error: unknown): WalletBridgeFailure {
  const code = (error as Readonly<{ code?: unknown }>).code;
  if (code === USER_REJECTED_REQUEST) {
    return "cancelled";
  }
  if (code === UNAUTHORIZED_REQUEST) {
    return "rejected";
  }
  return code === PROVIDER_DISCONNECTED || code === CHAIN_DISCONNECTED
    ? "unavailable"
    : "failed";
}

function firstAccount(response: unknown): string | undefined {
  if (typeof response !== "object" || response === null || !(0 in response)) {
    return undefined;
  }
  const first: unknown = Reflect.get(response, 0);
  return typeof first === "string" ? first : undefined;
}

export function browserBindingWalletBridge(
  provider: () => Eip1193Provider | undefined,
): BindingWalletBridge {
  return {
    async account(): Promise<WalletAccountOutcome> {
      const wallet = provider();
      if (wallet === undefined) {
        return { outcome: "unavailable" };
      }
      try {
        const response = await wallet.request({ method: "eth_requestAccounts" });
        const address = firstAccount(response);
        return address !== undefined && EVM_ADDRESS.test(address)
          ? { outcome: "approved", address }
          : { outcome: "failed" };
      } catch (error) {
        return { outcome: failure(error) };
      }
    },
    async sign(address, statement): Promise<WalletSignatureOutcome> {
      const wallet = provider();
      if (wallet === undefined) {
        return { outcome: "unavailable" };
      }
      try {
        const response = await wallet.request({
          method: "personal_sign",
          params: [statement, address],
        });
        return typeof response === "string" && EVM_SIGNATURE.test(response)
          ? { outcome: "approved", signature: response }
          : { outcome: "failed" };
      } catch (error) {
        return { outcome: failure(error) };
      }
    },
  };
}
