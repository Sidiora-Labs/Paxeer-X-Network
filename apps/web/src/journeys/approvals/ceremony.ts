import type {
  HumanApiClient,
  OpaqueCredential,
  OperationDigest,
  StepUpEvidence,
} from "../../api/index.ts";

export type PasskeyAuthenticator = (ceremony: OpaqueCredential) => Promise<OpaqueCredential>;

const PLUS = String.fromCharCode(43);

function base64FromUrl(value: string): string {
  const normalized = value.replaceAll("-", PLUS).replaceAll("_", "/");
  const missing = (4 - (normalized.length % 4)) % 4;
  return `${normalized}${"=".repeat(missing)}`;
}

export function bytesFromBase64Url(value: string): Uint8Array {
  const binary = atob(base64FromUrl(value));
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export function textFromBase64Url(value: string): string {
  return new TextDecoder().decode(bytesFromBase64Url(value));
}

export function base64UrlFromBytes(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) {
    binary = `${binary}${String.fromCharCode(byte)}`;
  }
  return btoa(binary).replaceAll(PLUS, "-").replaceAll("/", "_").replaceAll("=", "");
}

export function base64UrlFromText(value: string): string {
  return base64UrlFromBytes(new TextEncoder().encode(value));
}

export function browserPasskeyAuthenticator(): PasskeyAuthenticator {
  return async (ceremony) => {
    const options = JSON.parse(textFromBase64Url(ceremony)) as PublicKeyCredentialRequestOptionsJSON;
    const credential = await navigator.credentials.get({
      publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options),
    });
    if (credential === null) {
      throw new Error("The platform returned no credential for the ceremony");
    }
    const serialized: unknown = (credential as PublicKeyCredential).toJSON();
    return base64UrlFromText(JSON.stringify(serialized));
  };
}

export async function performStepUp(
  client: HumanApiClient,
  confirms: OperationDigest,
  authenticator: PasskeyAuthenticator,
): Promise<StepUpEvidence> {
  const challenge = await client.stepupBegin({ confirms });
  const challengeExpiry = Date.parse(challenge.expires_at);
  if (
    challenge.confirms !== confirms
    || !Number.isFinite(challengeExpiry)
    || challengeExpiry <= Date.now()
  ) {
    throw new Error("The step-up challenge is not bound to the current approval");
  }
  const credential = await authenticator(challenge.ceremony);
  const evidence = await client.stepupFinish(challenge.challenge_id, { credential });
  const evidenceExpiry = Date.parse(evidence.expires_at);
  if (
    evidence.challenge_id !== challenge.challenge_id
    || evidence.confirms !== confirms
    || !Number.isFinite(evidenceExpiry)
    || evidenceExpiry <= Date.now()
  ) {
    throw new Error("The step-up evidence is not bound to the current approval");
  }
  return evidence;
}
