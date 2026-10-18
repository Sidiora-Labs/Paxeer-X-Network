import {
  keccak256,
  AbiCoder,
  solidityPackedKeccak256,
  parseUnits,
  formatUnits,
  Numeric,
} from "ethers";
import type { BigNumberish } from "ethers";

export { parseUnits } from "ethers";

export type SignatureDomain = {
  name: string;
  version: string;
  chainId: number;
  verifyingContract: string;
};

export const base64url = function (aStr: string): string {
  return aStr.replace(/\+/g, "-").replace(/\//g, "_");
};

export function parseBrokerHash(brokerId: string) {
  return calculateStringHash(brokerId);
}

export function parseAccountId(userAddress: string, brokerId: string) {
  const abicoder = AbiCoder.defaultAbiCoder();
  return keccak256(
    abicoder.encode(
      ["address", "bytes32"],
      [userAddress, parseBrokerHash(brokerId)]
    )
  );
}

export function parseTokenHash(tokenSymbol: string) {
  return calculateStringHash(tokenSymbol);
}

export function calculateStringHash(input: string) {
  return solidityPackedKeccak256(["string"], [input]);
}

// export function parseUnits(amount: string) {
//   return parseUnits(amount, 6).toString();
// }

export function formatByUnits(
  amount: BigNumberish,
  unit: number | "ether" | "gwei" = "ether"
) {
  return formatUnits(amount, unit);
}

export function isHex(value: string): boolean {
  const hexRegex = /^[a-f0-9]+$/iu;
  return hexRegex.test(value);
}

export function isHexString(value: string): boolean {
  return typeof value === "string" && value.startsWith("0x") && isHex(value);
}

export const getGlobalObject = () => {
  if (typeof globalThis !== "undefined") {
    return globalThis;
  }
  if (typeof self !== "undefined") {
    return self;
  }
  if (typeof window !== "undefined") {
    return window;
  }
  // @ts-ignore
  if (typeof global !== "undefined") {
    // @ts-ignore
    return global;
  }
  throw new Error("cannot find the global object");
};

/// get timestamp
export const getTimestamp = (): number => {
  if (typeof window !== "undefined") {
    // @ts-ignore
    const timeOffset = getGlobalObject()?.__PAXEER_NETWORK_timestamp_offset;
    if (typeof timeOffset === "number") {
      return Date.now() + (timeOffset || 0);
    }
  }
  return Date.now();
};
