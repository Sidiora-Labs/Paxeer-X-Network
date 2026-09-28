import { createServer, type Server } from 'node:http';
import { generateKeyPairSync, sign as edSign, type KeyObject } from 'node:crypto';
import { SignJWT, exportJWK, generateKeyPair } from 'jose';

export interface IdentityProvider {
  url: string;
  mintUserToken(userId: string): Promise<string>;
  stop(): Promise<void>;
}

export async function startIdentityProvider(): Promise<IdentityProvider> {
  const { publicKey, privateKey } = await generateKeyPair('ES256');
  const kid = 'gateway-test-key';
  const jwk = { ...(await exportJWK(publicKey)), kid, alg: 'ES256', use: 'sig' };

  const server: Server = createServer((req, res) => {
    if (req.method === 'GET' && req.url === '/auth/v1/.well-known/jwks.json') {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ keys: [jwk] }));
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('identity provider did not bind');
  const url = `http://127.0.0.1:${address.port}`;

  return {
    url,
    async mintUserToken(userId: string): Promise<string> {
      return new SignJWT({})
        .setProtectedHeader({ alg: 'ES256', kid })
        .setIssuer(`${url}/auth/v1`)
        .setAudience('authenticated')
        .setSubject(userId)
        .setIssuedAt()
        .setExpirationTime('10m')
        .sign(privateKey);
    },
    async stop(): Promise<void> {
      await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
    },
  };
}

export interface AgentKey {
  did: string;
  publicKeyHex: string;
  privateKey: KeyObject;
  sign(message: Buffer): string;
}

export function newAgentKey(label: string): AgentKey {
  const { publicKey, privateKey } = generateKeyPairSync('ed25519');
  const spki = publicKey.export({ format: 'der', type: 'spki' });
  const publicKeyHex = Buffer.from(spki.subarray(spki.length - 32)).toString('hex');
  return {
    did: `did:matrix:${label}:${publicKeyHex.slice(0, 16)}`,
    publicKeyHex,
    privateKey,
    sign(message: Buffer): string {
      return edSign(null, message, privateKey).toString('hex');
    },
  };
}
