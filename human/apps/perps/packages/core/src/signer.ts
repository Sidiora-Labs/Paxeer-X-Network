import { PaxeerNetworkKeyStore } from "./keyStore";
import { base64url, getTimestamp } from "./utils";

import { Buffer } from "buffer";
// window.Buffer = window.Buffer || Buffer;

export type MessageFactor = {
  url: string;

  method: "GET" | "POST" | "PUT" | "DELETE";

  data?: any;
};

export type SignedMessagePayload = {
  "paxeer-network-key": string;
  "paxeer-network-timestamp": string;
  "paxeer-network-signature": string;
  "paxeer-network-account-id"?: string;
};

/**
 * Singer interface
 * @example
 * ```ts
 * const signer = new BaseSigner(keyStore);
 * const payload = await signer.sign({
 *  url: "https://api.paxeer-network.io/get_account?address=0x1234567890&brokerId=paxeer-network",
 *   method: "GET",
 *   data: {
 *     address: "0x1234567890",
 *     brokerId: "paxeer-network",
 *    },
 *  });
 *  ```
 */
export interface Signer {
  sign: (
    data: MessageFactor,
    timestamp?: number
  ) => Promise<SignedMessagePayload>;
  signText: (text: string) => Promise<{ signature: string; publicKey: string }>;
}

export class BaseSigner implements Signer {
  constructor(private readonly keyStore: PaxeerNetworkKeyStore) {}

  async sign(
    message: MessageFactor,
    timestamp = getTimestamp()
  ): Promise<SignedMessagePayload> {
    const _timestamp = timestamp.toString();

    let msgStr = [_timestamp, message.method.toUpperCase(), message.url].join(
      ""
    );

    if (message.data && Object.keys(message.data).length) {
      msgStr += JSON.stringify(message.data);
    }

    const { signature, publicKey } = await this.signText(msgStr);

    return {
      "paxeer-network-key": publicKey,
      "paxeer-network-timestamp": _timestamp,
      "paxeer-network-signature": signature,
    };
  }

  async signText(
    text: string
  ): Promise<{ signature: string; publicKey: string }> {
    const paxeerNetworkKeyPair = this.keyStore.getPaxeerNetworkKey();

    if (!paxeerNetworkKeyPair) {
      throw new Error("paxeerNetworkKeyPair is not defined");
    }

    const u8 = Buffer.from(text);

    const signature = await paxeerNetworkKeyPair.sign(u8 as unknown as Uint8Array);

    const signHex = Buffer.from(signature).toString("base64");

    const b64 = base64url(signHex);

    return {
      signature: b64,
      publicKey: await paxeerNetworkKeyPair.getPublicKey(),
    };
  }
}
