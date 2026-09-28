import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { X509Certificate, createHash, randomBytes, randomUUID } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer as createHttpServer, type Server as HttpServer } from 'node:http';
import { Agent, request as httpsRequest } from 'node:https';
import { createServer as createNetServer, type AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SignJWT, exportJWK, generateKeyPair, type KeyLike } from 'jose';
import { ADDR, BIND_SELECTOR } from '../support/chain.js';

const here = dirname(fileURLToPath(import.meta.url));
export const attestorModuleDir = resolve(here, '..', '..', '..', 'attestor');

export const CHAIN_ID = 125;
export const NATIVE_PER_TX_CAP_WEI = 1_000_000_000_000_000_000n;
const KERNEL_POLICY = { version: 1, defaults: { modules: { asset: [5] }, caps: { native: { per_operation: '6000000', daily: '8000000' } } } };

export interface Pki {
  dir: string;
  caFile: string;
  node(id: string): { cert: string; key: string; pin: string };
  clientCert: string;
  clientKey: string;
  operatorCaFile: string;
  operatorCert: string;
  operatorKey: string;
}

export interface Identity {
  url: string;
  issuer: string;
  mint(sub: string): Promise<string>;
  mintForeign(sub: string): Promise<string>;
  close(): Promise<void>;
}

export interface AttestorProcess {
  nodeId: string;
  apiUrl: string;
  child: ChildProcess;
  log: string[];
}

export interface GeneratedKey {
  keyId: string;
  address: `0x${string}`;
  publicKey: string;
  auditSequences: number[];
}

export interface AttestorNetwork {
  pki: Pki;
  nodes: AttestorProcess[];
  generate(keyId: string, owner: string): Promise<GeneratedKey>;
  health(node: AttestorProcess): Promise<{ status: number; body: Record<string, unknown> }>;
  stopNode(node: AttestorProcess): Promise<void>;
  stop(): Promise<void>;
}

function openssl(args: string[]): void {
  execFileSync('openssl', args, { stdio: 'pipe' });
}

function spkiPin(certPem: string): string {
  const der = new X509Certificate(certPem).publicKey.export({ type: 'spki', format: 'der' });
  return createHash('sha256').update(der).digest('hex');
}

export function makePki(nodeIds: string[]): Pki {
  const dir = mkdtempSync(join(tmpdir(), 'gateway-e2e-pki-'));
  const p = (n: string): string => join(dir, n);
  const ec = ['-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1', '-nodes'];
  openssl(['req', '-x509', ...ec, '-keyout', p('ca.key'), '-out', p('ca.crt'), '-days', '1', '-subj', '/CN=attestor-e2e-ca']);
  writeFileSync(
    p('leaf.ext'),
    'subjectAltName=IP:127.0.0.1\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth,clientAuth\n',
  );
  openssl(['req', '-x509', ...ec, '-keyout', p('operator-ca.key'), '-out', p('operator-ca.crt'), '-days', '1', '-subj', '/CN=attestor-e2e-operator-ca']);
  const issue = (name: string, ca = 'ca'): void => {
    openssl(['req', ...ec, '-keyout', p(`${name}.key`), '-out', p(`${name}.csr`), '-subj', `/CN=${name}`]);
    openssl([
      'x509', '-req', '-in', p(`${name}.csr`), '-CA', p(`${ca}.crt`), '-CAkey', p(`${ca}.key`), '-CAcreateserial',
      '-out', p(`${name}.crt`), '-days', '1', '-extfile', p('leaf.ext'),
    ]);
  };
  for (const id of nodeIds) issue(id);
  issue('wallet-gateway');
  issue('wallet-operator', 'operator-ca');
  const pins = new Map(nodeIds.map((id) => [id, spkiPin(readFileSync(p(`${id}.crt`), 'utf8'))]));
  return {
    dir,
    caFile: p('ca.crt'),
    node(id) {
      const pin = pins.get(id);
      if (!pin) throw new Error(`no certificate issued for ${id}`);
      return { cert: p(`${id}.crt`), key: p(`${id}.key`), pin };
    },
    clientCert: p('wallet-gateway.crt'),
    clientKey: p('wallet-gateway.key'),
    operatorCaFile: p('operator-ca.crt'),
    operatorCert: p('wallet-operator.crt'),
    operatorKey: p('wallet-operator.key'),
  };
}

export async function startIdentity(): Promise<Identity> {
  const kid = randomUUID();
  const { publicKey, privateKey } = await generateKeyPair('ES256');
  const foreign = await generateKeyPair('ES256');
  const jwk = { ...(await exportJWK(publicKey)), kid, alg: 'ES256', use: 'sig' };
  const server: HttpServer = createHttpServer((req, res) => {
    if (req.method === 'GET' && req.url === '/auth/v1/.well-known/jwks.json') {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ keys: [jwk] }));
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', () => r()));
  const url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const issuer = `${url}/auth/v1`;
  const sign = (sub: string, key: KeyLike): Promise<string> =>
    new SignJWT({ email: `${sub.slice(0, 8)}@example.com`, role: 'authenticated' })
      .setProtectedHeader({ alg: 'ES256', kid, typ: 'JWT' })
      .setIssuer(issuer)
      .setAudience('authenticated')
      .setSubject(sub)
      .setIssuedAt()
      .setNotBefore('0s')
      .setExpirationTime('10m')
      .sign(key);
  return {
    url,
    issuer,
    mint: (sub) => sign(sub, privateKey),
    mintForeign: (sub) => sign(sub, foreign.privateKey),
    close: () => new Promise<void>((r) => server.close(() => r())),
  };
}

function freePort(): Promise<number> {
  return new Promise((resolvePort, reject) => {
    const srv = createNetServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const port = (srv.address() as AddressInfo).port;
      srv.close(() => resolvePort(port));
    });
  });
}

export function buildAttestor(outDir: string): string {
  const bin = join(outDir, 'attestor');
  execFileSync('go', ['build', '-o', bin, './cmd/attestor'], {
    cwd: attestorModuleDir,
    stdio: 'pipe',
    env: { ...process.env, CGO_ENABLED: process.env.CGO_ENABLED ?? '0' },
  });
  return bin;
}

function mtlsAgent(pki: Pki, cert = pki.clientCert, key = pki.clientKey): Agent {
  return new Agent({
    cert: readFileSync(cert),
    key: readFileSync(key),
    ca: readFileSync(pki.caFile),
    rejectUnauthorized: true,
    minVersion: 'TLSv1.3',
  });
}

function call(
  agent: Agent,
  method: 'GET' | 'POST',
  url: string,
  body: unknown,
  timeoutMs: number,
): Promise<{ status: number; body: Record<string, unknown> }> {
  const payload = body === null ? null : JSON.stringify(body);
  return new Promise((resolveCall, reject) => {
    const req = httpsRequest(
      url,
      {
        method,
        agent,
        timeout: timeoutMs,
        headers: payload
          ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(payload) }
          : { accept: 'application/json' },
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on('data', (c: Buffer) => chunks.push(c));
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          try {
            resolveCall({ status: res.statusCode ?? 0, body: text ? (JSON.parse(text) as Record<string, unknown>) : {} });
          } catch {
            reject(new Error(`${url} answered non-JSON ${res.statusCode}: ${text}`));
          }
        });
        res.on('error', reject);
      },
    );
    req.on('timeout', () => req.destroy(new Error(`${url} timed out after ${timeoutMs} ms`)));
    req.on('error', reject);
    if (payload) req.write(payload);
    req.end();
  });
}

export async function startAttestorNetwork(opts: {
  identity: Identity;
  rpcUrl: string;
  size?: number;
  readyTimeoutMs?: number;
}): Promise<AttestorNetwork> {
  const size = opts.size ?? 5;
  const ids = Array.from({ length: size }, (_, i) => `node-${i + 1}`);
  const pki = makePki(ids);
  const work = mkdtempSync(join(tmpdir(), 'gateway-e2e-attestors-'));
  const bin = buildAttestor(work);
  const policyFile = join(work, 'policy.json');
  writeFileSync(
    policyFile,
    JSON.stringify({
      version: 1,
      defaults: {
        chain_id: CHAIN_ID,
        kinds: ['evm_tx', 'eip712', 'personal_message', 'lx_bind'],
        caps: { native: { per_transaction: NATIVE_PER_TX_CAP_WEI.toString(), daily: (NATIVE_PER_TX_CAP_WEI * 10n).toString() } },
        selectors: { [ADDR]: [BIND_SELECTOR] },
        rate_per_minute: 600,
      },
    }),
  );
  const kernelPolicyFile = join(work, 'kernel-policy.json');
  writeFileSync(kernelPolicyFile, JSON.stringify(KERNEL_POLICY));
  const ports = await Promise.all(ids.map(async () => ({ api: await freePort(), peer: await freePort() })));
  const nodes: AttestorProcess[] = [];
  for (const [i, id] of ids.entries()) {
    const dataDir = join(work, id);
    mkdirSync(dataDir, { recursive: true });
    const nodeKeyFile = join(work, `${id}.key`);
    writeFileSync(nodeKeyFile, randomBytes(32).toString('hex'), { mode: 0o600 });
    const others = ids.filter((o) => o !== id);
    const own = pki.node(id);
    const env: NodeJS.ProcessEnv = {
      PATH: process.env.PATH,
      HOME: process.env.HOME,
      ATTESTOR_NODE_ID: id,
      ATTESTOR_REGION: `region-${i + 1}`,
      ATTESTOR_LISTEN_ADDR: `127.0.0.1:${ports[i]!.api}`,
      ATTESTOR_PEER_LISTEN_ADDR: `127.0.0.1:${ports[i]!.peer}`,
      ATTESTOR_PEERS: others.map((o) => `${o}=127.0.0.1:${ports[ids.indexOf(o)]!.peer}`).join(','),
      ATTESTOR_PEER_PINS: others.map((o) => `${o}=${pki.node(o).pin}`).join(','),
      ATTESTOR_NODE_KEY_FILE: nodeKeyFile,
      ATTESTOR_DATA_DIR: dataDir,
      ATTESTOR_CHAIN_ID: String(CHAIN_ID),
      ATTESTOR_JWKS_URL: `${opts.identity.issuer}/.well-known/jwks.json`,
      ATTESTOR_JWT_ISSUER: opts.identity.issuer,
      ATTESTOR_JWT_AUDIENCE: 'authenticated',
      ATTESTOR_POLICY_FILE: policyFile,
      ATTESTOR_TLS_CERT_FILE: own.cert,
      ATTESTOR_TLS_KEY_FILE: own.key,
      ATTESTOR_TLS_CA_FILE: pki.caFile,
      ATTESTOR_OPERATOR_CA_FILE: pki.operatorCaFile,
      ATTESTOR_KERNEL_POLICY_FILE: kernelPolicyFile,
      ATTESTOR_RPC_URL: opts.rpcUrl,
      ATTESTOR_ACTIVITY_TYPES: '0x10005',
    };
    const child = spawn(bin, [], { env, stdio: ['ignore', 'pipe', 'pipe'] });
    const log: string[] = [];
    child.stdout?.on('data', (c: Buffer) => log.push(c.toString('utf8')));
    child.stderr?.on('data', (c: Buffer) => log.push(c.toString('utf8')));
    nodes.push({ nodeId: id, apiUrl: `https://127.0.0.1:${ports[i]!.api}`, child, log });
  }

  const agent = mtlsAgent(pki);
  const operator = mtlsAgent(pki, pki.operatorCert, pki.operatorKey);
  const health = (node: AttestorProcess) => call(agent, 'GET', `${node.apiUrl}/health`, null, 5_000);

  const deadline = Date.now() + (opts.readyTimeoutMs ?? 60_000);
  for (const node of nodes) {
    for (;;) {
      if (node.child.exitCode !== null) {
        throw new Error(`${node.nodeId} exited with ${node.child.exitCode}: ${node.log.join('')}`);
      }
      const answer = await health(node).catch(() => null);
      if (answer?.status === 200 && answer.body.ready === true) break;
      if (Date.now() > deadline) {
        throw new Error(`${node.nodeId} never became ready: ${JSON.stringify(answer?.body)} ${node.log.join('')}`);
      }
      await new Promise((r) => setTimeout(r, 250));
    }
  }

  const stopNode = async (node: AttestorProcess): Promise<void> => {
    if (node.child.exitCode !== null || node.child.signalCode !== null) return;
    const exited = new Promise<void>((r) => node.child.once('exit', () => r()));
    node.child.kill('SIGTERM');
    const timer = setTimeout(() => node.child.kill('SIGKILL'), 15_000);
    await exited;
    clearTimeout(timer);
  };

  return {
    pki,
    nodes,
    health,
    async generate(keyId, owner) {
      const sessionId = randomUUID();
      const request = { session_id: sessionId, key_id: keyId, curve: 'secp256k1', owner };
      const answers = await Promise.all(
        nodes.map((n) => call(agent, 'POST', `${n.apiUrl}/v1/keys/generate`, request, 600_000)),
      );
      for (const [i, a] of answers.entries()) {
        if (a.status !== 200) throw new Error(`${nodes[i]!.nodeId} refused keys.generate: ${a.status} ${JSON.stringify(a.body)}`);
      }
      const first = answers[0]!.body;
      for (const a of answers) {
        if (a.body.public_key !== first.public_key || a.body.address !== first.address) {
          throw new Error('participants disagree on the generated public key');
        }
      }
      if (answers.some((a) => a.body.refreshed !== true)) {
        const refresh = { session_id: randomUUID(), key_id: keyId };
        const refreshed = await Promise.all(
          nodes.map((n) => call(operator, 'POST', `${n.apiUrl}/v1/keys/refresh`, refresh, 600_000)),
        );
        for (const [i, a] of refreshed.entries()) {
          if (a.status !== 200 || a.body.refreshed !== true) {
            throw new Error(`${nodes[i]!.nodeId} refused keys.refresh: ${a.status} ${JSON.stringify(a.body)}`);
          }
        }
      }
      return {
        keyId,
        address: first.address as `0x${string}`,
        publicKey: first.public_key as string,
        auditSequences: answers.map((a) => a.body.audit_sequence as number),
      };
    },
    stopNode,
    async stop() {
      await Promise.all(nodes.map(stopNode));
      agent.destroy();
      operator.destroy();
      rmSync(work, { recursive: true, force: true });
      rmSync(pki.dir, { recursive: true, force: true });
    },
  };
}
