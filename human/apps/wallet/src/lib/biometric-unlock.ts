import { biometricUnlockRepository } from '@/platform/storage/repositories';

interface PrfExtensionResult {
  prf?: {
    enabled?: boolean;
    results?: {
      first?: ArrayBuffer;
    };
  };
}

interface PrfExtensionInput {
  prf: {
    eval: {
      first: BufferSource;
    };
  };
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function randomBytes(length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  crypto.getRandomValues(bytes);
  return bytes;
}

function encode(bytes: ArrayBuffer | Uint8Array): string {
  const source = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let binary = '';
  for (const byte of source) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function decode(value: string): Uint8Array {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=');
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

function asBuffer(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(
    bytes.byteOffset,
    bytes.byteOffset + bytes.byteLength,
  ) as ArrayBuffer;
}

function prfResult(credential: PublicKeyCredential): ArrayBuffer | null {
  const extensions =
    credential.getClientExtensionResults() as AuthenticationExtensionsClientOutputs &
      PrfExtensionResult;
  return extensions.prf?.results?.first ?? null;
}

async function evaluatePrf(
  credentialId: Uint8Array,
  prfSalt: Uint8Array,
): Promise<ArrayBuffer> {
  const credential = await navigator.credentials.get({
    publicKey: {
      challenge: asBuffer(randomBytes(32)),
      allowCredentials: [{
        id: asBuffer(credentialId),
        type: 'public-key',
        transports: ['internal'],
      }],
      userVerification: 'required',
      timeout: 60_000,
      extensions: {
        prf: { eval: { first: asBuffer(prfSalt) } },
      } as AuthenticationExtensionsClientInputs & PrfExtensionInput,
    },
  });
  if (!(credential instanceof PublicKeyCredential)) {
    throw new Error('Device authentication was cancelled.');
  }
  const result = prfResult(credential);
  if (!result) {
    throw new Error('This browser does not support biometric wallet unlock.');
  }
  return result;
}

async function aesKey(prf: ArrayBuffer): Promise<CryptoKey> {
  return crypto.subtle.importKey(
    'raw',
    prf,
    { name: 'AES-GCM' },
    false,
    ['encrypt', 'decrypt'],
  );
}

export function isBiometricUnlockSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    typeof PublicKeyCredential !== 'undefined' &&
    typeof navigator.credentials?.create === 'function' &&
    typeof navigator.credentials?.get === 'function'
  );
}

export function isBiometricUnlockEnrolled(): boolean {
  if (!isBiometricUnlockSupported()) return false;
  try {
    return biometricUnlockRepository.read() !== null;
  } catch {
    return false;
  }
}

export async function enrollBiometricUnlock(pin: string): Promise<void> {
  if (!/^\d{6}$/.test(pin)) throw new Error('Enter your 6-digit PIN.');
  if (!isBiometricUnlockSupported()) {
    throw new Error('Biometric unlock is unavailable in this browser.');
  }

  const prfSalt = randomBytes(32);
  const userId = randomBytes(32);
  const created = await navigator.credentials.create({
    publicKey: {
      challenge: asBuffer(randomBytes(32)),
      rp: { name: 'PaxPort Wallet' },
      user: {
        id: asBuffer(userId),
        name: 'paxport-wallet',
        displayName: 'PaxPort Wallet',
      },
      pubKeyCredParams: [
        { type: 'public-key', alg: -7 },
        { type: 'public-key', alg: -257 },
      ],
      authenticatorSelection: {
        authenticatorAttachment: 'platform',
        residentKey: 'preferred',
        userVerification: 'required',
      },
      attestation: 'none',
      timeout: 60_000,
      extensions: {
        prf: { eval: { first: asBuffer(prfSalt) } },
      } as AuthenticationExtensionsClientInputs & PrfExtensionInput,
    },
  });
  if (!(created instanceof PublicKeyCredential)) {
    throw new Error('Biometric enrollment was cancelled.');
  }

  const credentialId = new Uint8Array(created.rawId);
  const prf = prfResult(created) ?? await evaluatePrf(credentialId, prfSalt);
  const key = await aesKey(prf);
  const iv = randomBytes(12);
  const plaintext = encoder.encode(pin);
  try {
    const ciphertext = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: asBuffer(iv) },
      key,
      asBuffer(plaintext),
    );
    biometricUnlockRepository.write({
      credentialId: encode(credentialId),
      prfSalt: encode(prfSalt),
      iv: encode(iv),
      ciphertext: encode(ciphertext),
      createdAt: Date.now(),
    });
  } finally {
    plaintext.fill(0);
  }
}

export async function recoverPinWithBiometrics(): Promise<string> {
  const record = biometricUnlockRepository.read();
  if (!record) throw new Error('Biometric unlock is not enabled.');
  if (!isBiometricUnlockSupported()) {
    throw new Error('Biometric unlock is unavailable in this browser.');
  }
  const prf = await evaluatePrf(
    decode(record.credentialId),
    decode(record.prfSalt),
  );
  const key = await aesKey(prf);
  const plaintext = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: asBuffer(decode(record.iv)) },
    key,
    asBuffer(decode(record.ciphertext)),
  );
  const pin = decoder.decode(plaintext);
  if (!/^\d{6}$/.test(pin)) {
    biometricUnlockRepository.remove();
    throw new Error('Biometric unlock data is invalid. Enroll again.');
  }
  return pin;
}

export function disableBiometricUnlock(): void {
  biometricUnlockRepository.remove();
}
