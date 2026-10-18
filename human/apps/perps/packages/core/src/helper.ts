import { DEFAUL_PAXEER_NETWORK_KEY_SCOPE, definedTypes } from "@paxeer-network/types";
import { LocalStorageStore, MockKeyStore } from "./keyStore";
import { BaseSigner } from "./signer";
import { getTimestamp, SignatureDomain } from "./utils";

export const getMockSigner = (secretKey?: string) => {
  const mockKeyStore = new MockKeyStore(
    secretKey || "AFmQSju4FhDwG93cMdKogcnKx7SWmViDtDv5PVzfvRDF",
    // "c24fe227663f5a73493cad3f4049514f70623177272d57fffa8cb895fa1f92de"
  );

  return new BaseSigner(mockKeyStore);
};

export const getDefaultSigner = () => {
  if (typeof window === "undefined") {
    throw new Error("the default signer only supports browsers.");
  }

  const localStorageStore = new LocalStorageStore("");

  return new BaseSigner(localStorageStore);
};

export function getVerifyingContract() {
  return "0x8794E7260517B1766fc7b55cAfcd56e6bf08600e";
}

export function getDomain(chainId: number, onChainDomain?: boolean) {
  return {
    name: "paxeer-network",
    version: "1",
    chainId,
    verifyingContract: onChainDomain
      ? getVerifyingContract()
      : "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
  };
}

/**
 * generate `registerAccount` data and to be signed message structure
 */
export function generateRegisterAccountMessage(inputs: {
  chainId: number;
  registrationNonce: number;
  brokerId: string;
  timestamp?: number;
}) {
  const {
    chainId,
    registrationNonce,
    brokerId,
    timestamp = getTimestamp(),
  } = inputs;
  // const now = Date.now();
  const primaryType = "Registration";
  const message = {
    brokerId,
    chainId,
    timestamp,
    registrationNonce,
  };

  const typeDefinition = {
    EIP712Domain: definedTypes.EIP712Domain,
    [primaryType]: definedTypes[primaryType],
  };

  const toSignatureMessage = {
    domain: getDomain(chainId),
    message,
    primaryType,
    types: typeDefinition,
  };

  return [message, toSignatureMessage] as const;
}

/**
 * generate `addPaxeerNetworkKey` data and to be signed message structure
 */
export function generateAddPaxeerNetworkKeyMessage(inputs: {
  publicKey: string;
  chainId: number;
  brokerId: string;
  primaryType: keyof typeof definedTypes;
  expiration?: number;
  timestamp?: number;
  scope?: string;
  tag?: string;
  subAccountId?: string;
}) {
  const {
    publicKey,
    chainId,
    primaryType,
    brokerId,
    expiration = 365,
    timestamp = getTimestamp(),
    scope,
    tag,
    subAccountId,
  } = inputs;
  // const now = Date.now();
  // message;
  const message = {
    brokerId,
    paxeerNetworkKey: publicKey,
    scope: scope || DEFAUL_PAXEER_NETWORK_KEY_SCOPE,
    chainId,
    timestamp,
    expiration: timestamp + 1000 * 60 * 60 * 24 * expiration,
    ...(typeof tag !== "undefined" ? { tag } : {}),
    ...(typeof subAccountId !== "undefined" ? { subAccountId } : {}),
  };

  const typeDefinition = {
    EIP712Domain: definedTypes.EIP712Domain,
    [primaryType]: definedTypes[primaryType],
  };

  const toSignatureMessage = {
    domain: getDomain(chainId),
    message,
    primaryType,
    types: typeDefinition,
  };

  return [message, toSignatureMessage] as const;
}

/**
 * generate `settle` data and to be signed message structure
 */
export function generateSettleMessage(inputs: {
  chainId: number;
  brokerId: string;
  settlePnlNonce: string;
  domain: SignatureDomain;
}) {
  const { chainId, settlePnlNonce, domain, brokerId } = inputs;
  const primaryType = "SettlePnl";
  const timestamp = getTimestamp();

  const typeDefinition = {
    EIP712Domain: definedTypes.EIP712Domain,
    [primaryType]: definedTypes[primaryType],
  };

  const message = {
    brokerId,
    chainId: chainId,
    timestamp: timestamp,
    settleNonce: settlePnlNonce,
  };

  const toSignatureMessage = {
    domain,
    message,
    primaryType,
    types: typeDefinition,
  };

  return [message, toSignatureMessage] as const;
}

/**
 * generate `dexRequest` data and to be signed message structure
 */
export function generateDexRequestMessage(inputs: {
  chainId: number;
  payloadType: number;
  nonce: string;
  receiver: string;
  amount: string;
  vaultId: string;
  token: string;
  dexBrokerId: string;
  domain: SignatureDomain;
  timestamp?: number;
}) {
  const {
    chainId,
    payloadType,
    nonce,
    receiver,
    amount,
    vaultId,
    token,
    dexBrokerId,
    domain,
    timestamp = getTimestamp(),
  } = inputs;

  const primaryType = "DexRequest";

  const typeDefinition = {
    EIP712Domain: definedTypes.EIP712Domain,
    [primaryType]: definedTypes[primaryType],
  };

  const message = {
    payloadType,
    nonce,
    receiver,
    amount,
    vaultId,
    token,
    dexBrokerId,
  };

  const toSignatureMessage = {
    domain,
    message,
    primaryType,
    types: typeDefinition,
  };

  return [message, toSignatureMessage] as const;
}
