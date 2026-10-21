export class CeremonyCancelled extends Error {
  constructor() {
    super("the platform credential ceremony did not complete");
    this.name = "CeremonyCancelled";
  }
}

const PLUS = String.fromCharCode(43);
const SLASH = String.fromCharCode(47);

function fromBase64Url(value: string): ArrayBuffer {
  const normalized = value.replaceAll("-", PLUS).replaceAll("_", SLASH);
  const targetLength = normalized.length + ((4 - (normalized.length % 4)) % 4);
  const padded = normalized.padEnd(targetLength, "=");
  const binary = atob(padded);
  const buffer = new ArrayBuffer(binary.length);
  const bytes = new Uint8Array(buffer);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return buffer;
}

function toBase64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) {
    binary = `${binary}${String.fromCharCode(byte)}`;
  }
  return btoa(binary).replaceAll(PLUS, "-").replaceAll(SLASH, "_").replace(/=*$/u, "");
}

function decodeCeremony(ceremony: string): Readonly<Record<string, unknown>> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(fromBase64Url(ceremony)));
  } catch {
    throw new CeremonyCancelled();
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new CeremonyCancelled();
  }
  return parsed as Readonly<Record<string, unknown>>;
}

function stringPart(source: Readonly<Record<string, unknown>>, name: string): string {
  const value = source[name];
  if (typeof value !== "string" || value.length === 0) {
    throw new CeremonyCancelled();
  }
  return value;
}

function recordPart(source: Readonly<Record<string, unknown>>, name: string): Readonly<Record<string, unknown>> {
  const value = source[name];
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new CeremonyCancelled();
  }
  return value as Readonly<Record<string, unknown>>;
}

function encodeCredential(credentialJson: Record<string, unknown>): string {
  const bytes = new TextEncoder().encode(JSON.stringify(credentialJson));
  const copy = new ArrayBuffer(bytes.length);
  new Uint8Array(copy).set(bytes);
  return toBase64Url(copy);
}

function stringEnum<T extends string>(
  source: Readonly<Record<string, unknown>>,
  name: string,
  allowed: readonly T[],
): T | undefined {
  const value = source[name];
  return typeof value === "string" && allowed.includes(value as T) ? value as T : undefined;
}

function credentialParameters(options: Readonly<Record<string, unknown>>): PublicKeyCredentialParameters[] {
  const declared = options["pubKeyCredParams"];
  if (!Array.isArray(declared)) {
    throw new CeremonyCancelled();
  }
  const parameters: PublicKeyCredentialParameters[] = [];
  for (const entry of declared as readonly unknown[]) {
    if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
      throw new CeremonyCancelled();
    }
    const shape = entry as Readonly<Record<string, unknown>>;
    if (shape["type"] !== "public-key" || typeof shape["alg"] !== "number") {
      throw new CeremonyCancelled();
    }
    parameters.push({ type: "public-key", alg: shape["alg"] });
  }
  if (parameters.length === 0) {
    throw new CeremonyCancelled();
  }
  return parameters;
}

function credentialDescriptors(
  options: Readonly<Record<string, unknown>>,
  name: string,
): PublicKeyCredentialDescriptor[] {
  const declared = options[name];
  if (declared === undefined) {
    return [];
  }
  if (!Array.isArray(declared)) {
    throw new CeremonyCancelled();
  }
  const descriptors: PublicKeyCredentialDescriptor[] = [];
  for (const entry of declared as readonly unknown[]) {
    if (typeof entry !== "object" || entry === null || Array.isArray(entry)) {
      throw new CeremonyCancelled();
    }
    const shape = entry as Readonly<Record<string, unknown>>;
    if (shape["type"] !== "public-key" || typeof shape["id"] !== "string" || shape["id"].length === 0) {
      throw new CeremonyCancelled();
    }
    const transports = shape["transports"];
    descriptors.push({
      type: "public-key",
      id: fromBase64Url(shape["id"]),
      ...(Array.isArray(transports) && transports.every((value) => typeof value === "string")
        ? { transports: transports as AuthenticatorTransport[] }
        : {}),
    });
  }
  return descriptors;
}

function ceremonyTimeout(options: Readonly<Record<string, unknown>>): Readonly<{ timeout?: number }> {
  const declared = options["timeout"];
  return typeof declared === "number" ? { timeout: declared } : {};
}

export async function performRegistrationCeremony(ceremony: string): Promise<string> {
  const options = decodeCeremony(ceremony);
  const user = recordPart(options, "user");
  const relyingParty = recordPart(options, "rp");
  const selection = recordPart(options, "authenticatorSelection");
  const attachment = stringEnum(selection, "authenticatorAttachment", ["platform", "cross-platform"] as const);
  const residentKey = stringEnum(selection, "residentKey", ["discouraged", "preferred", "required"] as const);
  const userVerification = stringEnum(selection, "userVerification", ["discouraged", "preferred", "required"] as const);
  const attestation = stringEnum(options, "attestation", ["none", "indirect", "direct", "enterprise"] as const);
  const publicKey: PublicKeyCredentialCreationOptions = {
    rp: {
      id: stringPart(relyingParty, "id"),
      name: stringPart(relyingParty, "name"),
    },
    user: {
      id: fromBase64Url(stringPart(user, "id")),
      name: stringPart(user, "name"),
      displayName: stringPart(user, "displayName"),
    },
    challenge: fromBase64Url(stringPart(options, "challenge")),
    pubKeyCredParams: credentialParameters(options),
    excludeCredentials: credentialDescriptors(options, "excludeCredentials"),
    ...ceremonyTimeout(options),
    ...(attestation === undefined ? {} : { attestation }),
    authenticatorSelection: {
      ...(attachment === undefined ? {} : { authenticatorAttachment: attachment }),
      ...(residentKey === undefined ? {} : {
        residentKey,
        requireResidentKey: residentKey === "required",
      }),
      ...(userVerification === undefined ? {} : { userVerification }),
    },
  };
  let credential: Credential | null;
  try {
    credential = await navigator.credentials.create({ publicKey });
  } catch {
    throw new CeremonyCancelled();
  }
  if (typeof PublicKeyCredential === "undefined" || !(credential instanceof PublicKeyCredential)) {
    throw new CeremonyCancelled();
  }
  const response = credential.response;
  if (
    typeof AuthenticatorAttestationResponse === "undefined"
    || !(response instanceof AuthenticatorAttestationResponse)
  ) {
    throw new CeremonyCancelled();
  }
  return encodeCredential({
    id: toBase64Url(credential.rawId),
    transports: response.getTransports(),
    clientDataJSON: toBase64Url(response.clientDataJSON),
    attestationObject: toBase64Url(response.attestationObject),
  });
}

export async function performAssertionCeremony(ceremony: string): Promise<string> {
  const options = decodeCeremony(ceremony);
  const userVerification = stringEnum(options, "userVerification", ["discouraged", "preferred", "required"] as const);
  const publicKey: PublicKeyCredentialRequestOptions = {
    challenge: fromBase64Url(stringPart(options, "challenge")),
    rpId: stringPart(options, "rpId"),
    ...ceremonyTimeout(options),
    ...(userVerification === undefined ? {} : { userVerification }),
    allowCredentials: credentialDescriptors(options, "allowCredentials"),
  };
  let credential: Credential | null;
  try {
    credential = await navigator.credentials.get({ publicKey });
  } catch {
    throw new CeremonyCancelled();
  }
  if (typeof PublicKeyCredential === "undefined" || !(credential instanceof PublicKeyCredential)) {
    throw new CeremonyCancelled();
  }
  const response = credential.response;
  if (
    typeof AuthenticatorAssertionResponse === "undefined"
    || !(response instanceof AuthenticatorAssertionResponse)
  ) {
    throw new CeremonyCancelled();
  }
  const userHandle = response.userHandle;
  return encodeCredential({
    id: toBase64Url(credential.rawId),
    clientDataJSON: toBase64Url(response.clientDataJSON),
    authenticatorData: toBase64Url(response.authenticatorData),
    signature: toBase64Url(response.signature),
    ...(userHandle === null ? {} : { userHandle: toBase64Url(userHandle) }),
  });
}
