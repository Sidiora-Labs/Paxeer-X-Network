"use server";

import { GatewayUnavailableError } from "../../api/gateway";
import { perpsPreflight } from "../../api/precompiles";

export type PerpsPreflightAnswer = Readonly<{ ok: true }> | Readonly<{ ok: false; detail: string }>;

const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/u;
const CALLDATA = /^0x[0-9a-fA-F]{8}(?:[0-9a-fA-F]{64})*$/u;

/** Runs a perps write against 0x1015 as `from` before the wallet opens, answering the precompile's revert reason. */
export async function preflightPerpsWrite(from: string, data: string): Promise<PerpsPreflightAnswer> {
  if (!EVM_ADDRESS.test(from) || !CALLDATA.test(data)) {
    return { ok: false, detail: "The perps write is malformed." };
  }
  try {
    const refused = await perpsPreflight(from, data);
    return refused === undefined ? { ok: true } : { ok: false, detail: refused };
  } catch (error) {
    if (error instanceof GatewayUnavailableError) {
      return { ok: false, detail: error.message };
    }
    throw error;
  }
}
