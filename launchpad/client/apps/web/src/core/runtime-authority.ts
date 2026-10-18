'use client';

import { createContext, createElement, lazy, Suspense, useContext, useEffect, useState, type ReactNode } from 'react';
import { getAddress, type Address, type Hash } from 'viem';
import type { PublicNetworkConfigurationV1, WalletNetworkAuthorityV2 } from './wallet-sdk/backend';
import type { TradeDeployment, CodePin } from '../widgets/trading/transaction-controls/providers';
import { CORE_DEPLOYMENT } from './network/deployment';
import { reportUserFacingError } from './user-facing-error';

export interface DeploymentRevisionV2 {
  schemaVersion: 2; deploymentVersion: string; manifestSha256: string;
  capabilitySha256: string; contractSupportSha256: string;
  serialInterfaceSha256: string; revisionSha256: string;
}
export interface PrivateFixtureRoleIdentityV1 {
  kind: 'direct' | 'erc1967' | 'beacon';
  address: Address; runtimeHash: Hash;
  implementationAddress?: Address; implementationHash?: Hash;
}
export interface PrivateFixtureAuthorityProfileV1 {
  schemaVersion: 1; profile: 'PRIVATE_FIXTURE'; chainId: 125;
  applicationOrigin: string; sourceManifestSha256: string;
  revision: Readonly<DeploymentRevisionV2>; networkSha256: string;
  publicRPC: string; explorerBase: string; expectedBeaconOwner: Address;
  roles: Readonly<Record<RuntimeAuthorityRoleV1, Readonly<PrivateFixtureRoleIdentityV1>>>;
  sourceArtifacts: Readonly<{ chainSha256: string; compilerArtifactsSha256: string; reviewedManifestSha256: string }>;
}
interface RoleIdentity {
  role: string; address: Address; runtimeHash: Hash;
  implementationAddress?: Address; implementationHash?: Hash;
  beaconAddress?: Address; verified: boolean; issues?: unknown[];
}
interface ActivationAttestation {
  manifestHash: string; deploymentVersion: string; blockHash: Hash; blockNumber: number;
  observedAt: string; blockTime: string; identities: Record<string, RoleIdentity>;
  actualOwner: Address; pendingOwner: Address; creationFeeRaw: string;
  settlementAddress: Address; usdlDecimals: number; eligible: boolean; issues?: unknown[];
}
interface AuthorityEnvelope {
  schemaVersion: 2;
  serialExport: { schemaVersion: 2; revision: DeploymentRevisionV2; network: PublicNetworkConfigurationV1; networkSha256: string };
  attestation: ActivationAttestation;
  committedHead: { number: string; hash: Hash };
}
export interface RuntimeAuthority {
  revision: Readonly<DeploymentRevisionV2>;
  expiresAt: number;
  network: WalletNetworkAuthorityV2;
  tradeDeployment: TradeDeployment;
  preflight(family: 'creation' | 'plain_quote' | 'trading' | 'nft_fees', request: RuntimePreflightRequest, signal: AbortSignal): Promise<Readonly<Record<string, unknown>>>;
}
export interface RuntimePreflightRequest {
  blockHash: Hash; trader: Address; recipient: Address; pool?: Address;
  amountIn?: string; isBuy?: boolean; creationPath?: 'Router' | 'SidioraFactory';
  name?: string; symbol?: string; feeStrategy?: number; optical?: Address; nftId?: string;
}
const digests = /^[0-9a-f]{64}$/;
const hashes = /^0x[0-9a-fA-F]{64}$/;
const roles = { EventEmitter: 'eventEmitter', ProtocolConfig: 'protocolConfig', Router: 'router', Quoter: 'quoter', SidioraFactory: 'factory', PoolRegistry: 'poolRegistry', FeeAccumulator: 'feeAccumulator', SidioraNFT: 'nft', FeesRouter: 'feesRouter', OpticalRegistry: 'opticalRegistry', PoolBeacon: 'poolBeacon', USDL: 'usdl' } as const;
export type RuntimeAuthorityRoleV1 = keyof typeof roles;
function loopbackHTTPS(value: unknown, originOnly = false): string {
  if (typeof value !== 'string' || !value || value.length > 2048 || value.trim() !== value) throw new Error('Approved local network input is unavailable');
  const url = new URL(value);
  if (url.protocol !== 'https:' || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) || url.username || url.password || url.search || url.hash || originOnly && url.origin !== value) throw new Error('Approved local network input is unavailable');
  return value;
}
function fixtureDigest(value: unknown): string {
  if (typeof value !== 'string' || !digests.test(value) || /^0{64}$/.test(value)) throw new Error('Reviewed local network source evidence is unavailable');
  return value;
}
function fixtureAddress(value: unknown): Address {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(value) || /^0x0{40}$/.test(value)) throw new Error('Reviewed local contract identity is unavailable');
  return getAddress(value);
}
function fixtureCodeHash(value: unknown): Hash {
  if (typeof value !== 'string' || !hashes.test(value) || /^0x0{64}$/.test(value)) throw new Error('Reviewed local contract identity is unavailable');
  return value.toLowerCase() as Hash;
}
export function parsePrivateFixtureAuthorityProfileV1(input: unknown): PrivateFixtureAuthorityProfileV1 {
  if (typeof input === 'string') {
    if (!input || new TextEncoder().encode(input).byteLength > 64 * 1024) throw new Error('Reviewed local network input is unavailable');
    input = JSON.parse(input) as unknown;
  }
  const profile = object(input), revision = object(profile.revision), artifacts = object(profile.sourceArtifacts), identities = object(profile.roles);
  if (profile.schemaVersion !== 1 || profile.profile !== 'PRIVATE_FIXTURE' || profile.chainId !== 125 || revision.schemaVersion !== 2 || typeof revision.deploymentVersion !== 'string' || !revision.deploymentVersion || revision.deploymentVersion.length > 128) throw new Error('Reviewed local network input is unavailable');
  const parsedRevision: DeploymentRevisionV2 = { schemaVersion: 2, deploymentVersion: revision.deploymentVersion, manifestSha256: fixtureDigest(revision.manifestSha256), capabilitySha256: fixtureDigest(revision.capabilitySha256), contractSupportSha256: fixtureDigest(revision.contractSupportSha256), serialInterfaceSha256: fixtureDigest(revision.serialInterfaceSha256), revisionSha256: fixtureDigest(revision.revisionSha256) };
  if (Object.keys(identities).length !== Object.keys(roles).length || Object.keys(identities).some(role => !Object.hasOwn(roles, role))) throw new Error('Reviewed local role inventory is unavailable');
  const parsedRoles = Object.fromEntries((Object.keys(roles) as RuntimeAuthorityRoleV1[]).map(role => {
    const identity = object(identities[role]);
    if (!['direct', 'erc1967', 'beacon'].includes(String(identity.kind))) throw new Error('Reviewed local contract identity is unavailable');
    const parsed: PrivateFixtureRoleIdentityV1 = { kind: identity.kind as PrivateFixtureRoleIdentityV1['kind'], address: fixtureAddress(identity.address), runtimeHash: fixtureCodeHash(identity.runtimeHash) };
    if (parsed.kind === 'direct') {
      if (identity.implementationAddress !== undefined || identity.implementationHash !== undefined) throw new Error('Reviewed local direct contract identity is inconsistent');
    } else {
      parsed.implementationAddress = fixtureAddress(identity.implementationAddress);
      parsed.implementationHash = fixtureCodeHash(identity.implementationHash);
    }
    if (['Router', 'Quoter', 'PoolRegistry', 'EventEmitter'].includes(role) && parsed.kind === 'beacon') throw new Error('Reviewed local execution identity is unsupported');
    return [role, Object.freeze(parsed)];
  })) as Record<RuntimeAuthorityRoleV1, Readonly<PrivateFixtureRoleIdentityV1>>;
  return Object.freeze({ schemaVersion: 1, profile: 'PRIVATE_FIXTURE', chainId: 125, applicationOrigin: loopbackHTTPS(profile.applicationOrigin, true), sourceManifestSha256: fixtureDigest(profile.sourceManifestSha256), revision: Object.freeze(parsedRevision), networkSha256: fixtureDigest(profile.networkSha256), publicRPC: loopbackHTTPS(profile.publicRPC), explorerBase: loopbackHTTPS(profile.explorerBase), expectedBeaconOwner: fixtureAddress(profile.expectedBeaconOwner), roles: Object.freeze(parsedRoles), sourceArtifacts: Object.freeze({ chainSha256: fixtureDigest(artifacts.chainSha256), compilerArtifactsSha256: fixtureDigest(artifacts.compilerArtifactsSha256), reviewedManifestSha256: fixtureDigest(artifacts.reviewedManifestSha256) }) });
}
function assertFixtureOrigin(profile: PrivateFixtureAuthorityProfileV1) {
  if (typeof window === 'undefined' || loopbackHTTPS(window.location.origin, true) !== profile.applicationOrigin) throw new Error('Approved local network origin is unavailable');
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Network authority response is invalid');
  return value as Record<string, unknown>;
}
function goJSON(value: unknown): string {
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, value => `\\u${value.charCodeAt(0).toString(16).padStart(4, '0')}`);
}
async function sha(value: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(goJSON(value));
  return [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(byte => byte.toString(16).padStart(2, '0')).join('');
}
async function readJSON(path: string, signal: AbortSignal, body?: unknown): Promise<unknown> {
  signal.throwIfAborted();
  const controller = new AbortController();
  const cancel = () => controller.abort(signal.reason);
  signal.addEventListener('abort', cancel, { once: true });
  const timer = setTimeout(() => controller.abort(new DOMException('Network authority timed out', 'TimeoutError')), 15_000);
  try { return await readJSONBody(path, controller.signal, body); }
  finally { clearTimeout(timer); signal.removeEventListener('abort', cancel); }
}
async function readJSONBody(path: string, signal: AbortSignal, body?: unknown): Promise<unknown> {
  if (window.location.protocol !== 'https:') throw new Error('Network authority requires the HTTPS application origin');
  const response = await fetch(new URL(path, window.location.origin), { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal, headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  if (!response.ok) throw new Error(`Network authority is unavailable (${response.status})`);
  if (!response.headers.get('content-type')?.toLowerCase().startsWith('application/json')) throw new Error('Network authority did not return JSON');
  const reader = response.body?.getReader();
  if (!reader) throw new Error('Network authority response is empty');
  const chunks: Uint8Array[] = []; let length = 0;
  try {
    while (true) {
      const next = await reader.read(); signal.throwIfAborted();
      if (next.done) break;
      length += next.value.byteLength;
      if (length > 1024 * 1024) throw new Error('Network authority response exceeded its limit');
      chunks.push(next.value);
    }
  } finally { await reader.cancel().catch(() => {}); }
  const bytes = new Uint8Array(length); let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
  return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)) as unknown;
}
async function readAuthority(signal: AbortSignal, fixture?: PrivateFixtureAuthorityProfileV1): Promise<AuthorityEnvelope> {
  if (fixture) assertFixtureOrigin(fixture);
  const unknown = object(await readJSON('/v2/network/authority', signal));
  const serial = object(unknown.serialExport), revision = object(serial.revision), network = object(serial.network), attestation = object(unknown.attestation), head = object(unknown.committedHead);
  if (unknown.schemaVersion !== 2 || serial.schemaVersion !== 2 || revision.schemaVersion !== 2 || network.schemaVersion !== 1 || network.chainId !== 125) throw new Error('Unsupported network authority version');
  for (const key of ['manifestSha256', 'capabilitySha256', 'contractSupportSha256', 'serialInterfaceSha256', 'revisionSha256']) if (typeof revision[key] !== 'string' || !digests.test(revision[key] as string)) throw new Error('Network revision is invalid');
  if (typeof revision.deploymentVersion !== 'string' || !revision.deploymentVersion || revision.deploymentVersion.length > 128 || typeof serial.networkSha256 !== 'string' || !digests.test(serial.networkSha256)) throw new Error('Network revision is invalid');
  if (typeof network.sourceManifestSha256 !== 'string' || network.sourceManifestSha256 !== (fixture ? fixture.sourceManifestSha256 : CORE_DEPLOYMENT.manifestSha256) || network.manifestHash !== revision.manifestSha256 || network.deploymentVersion !== revision.deploymentVersion || network.creationFeeRaw !== '1000000') throw new Error('Network authority differs from the reviewed deployment');
  if (fixture && (Object.entries(fixture.revision).some(([key, value]) => revision[key] !== value) || serial.networkSha256 !== fixture.networkSha256 || network.publicRPC !== fixture.publicRPC || network.explorerBase !== fixture.explorerBase || Object.keys(object(network.addresses)).length !== Object.keys(fixture.roles).length)) throw new Error('Network authority differs from the reviewed local deployment');
  const native = object(network.native), settlement = object(network.settlement), addresses = object(network.addresses), identities = object(attestation.identities);
  if (native.symbol !== 'PAX' || native.decimals !== 18 || settlement.symbol !== 'USDL' || settlement.decimals !== 6) throw new Error('Network asset policy is invalid');
  if (Object.keys(addresses).length > 64) throw new Error('Network role inventory is invalid');
  for (const [role, value] of Object.entries(addresses)) if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$/.test(role) || typeof value !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(value) || /^0x0{40}$/.test(value)) throw new Error('Network role inventory is invalid');
  for (const endpoint of [network.publicRPC, network.explorerBase]) {
    if (typeof endpoint !== 'string' || endpoint.length > 2048) throw new Error('Public network endpoint is unavailable');
    const url = new URL(endpoint);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) throw new Error('Public network endpoint is not approved');
  }
  for (const role of Object.keys(roles) as (keyof typeof roles)[]) {
    const field = roles[role];
    const identity = object(identities[role]);
    if (typeof addresses[role] !== 'string' || getAddress(addresses[role] as string) !== getAddress(fixture ? fixture.roles[role].address : CORE_DEPLOYMENT[field]) || identity.role !== role || typeof identity.address !== 'string' || getAddress(identity.address) !== getAddress(addresses[role] as string) || identity.verified !== true || typeof identity.runtimeHash !== 'string' || !hashes.test(identity.runtimeHash) || (identity.issues !== undefined && (!Array.isArray(identity.issues) || identity.issues.length))) throw new Error(`Network identity is unavailable for ${role}`);
    if (fixture) {
      const expected = fixture.roles[role];
      if (identity.runtimeHash.toLowerCase() !== expected.runtimeHash || expected.kind === 'direct' && (identity.implementationAddress !== undefined || identity.implementationHash !== undefined) || expected.kind !== 'direct' && (typeof identity.implementationAddress !== 'string' || getAddress(identity.implementationAddress) !== expected.implementationAddress || typeof identity.implementationHash !== 'string' || identity.implementationHash.toLowerCase() !== expected.implementationHash)) throw new Error(`Reviewed network identity differs for ${role}`);
    } else if (role !== 'USDL' && (typeof identity.implementationAddress !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(identity.implementationAddress) || /^0x0{40}$/.test(identity.implementationAddress) || typeof identity.implementationHash !== 'string' || !hashes.test(identity.implementationHash))) throw new Error(`Implementation identity is unavailable for ${role}`);
  }
  if (typeof head.number !== 'string' || !/^(0|[1-9][0-9]{0,19})$/.test(head.number) || typeof head.hash !== 'string' || !hashes.test(head.hash) || !Number.isSafeInteger(attestation.blockNumber) || BigInt(attestation.blockNumber as number) !== BigInt(head.number) || attestation.blockHash !== head.hash || attestation.manifestHash !== network.manifestHash || attestation.deploymentVersion !== network.deploymentVersion || attestation.eligible !== true || (attestation.issues !== undefined && (!Array.isArray(attestation.issues) || attestation.issues.length)) || attestation.creationFeeRaw !== '1000000' || attestation.usdlDecimals !== 6 || typeof attestation.settlementAddress !== 'string' || getAddress(attestation.settlementAddress) !== getAddress(addresses.USDL as string) || attestation.pendingOwner !== '0x0000000000000000000000000000000000000000') throw new Error('Committed network activation is unavailable');
  if (fixture && (typeof attestation.actualOwner !== 'string' || getAddress(attestation.actualOwner) !== fixture.expectedBeaconOwner)) throw new Error('Reviewed network governance identity differs');
  const now = Date.now(), observed = Date.parse(String(attestation.observedAt)), blockTime = Date.parse(String(attestation.blockTime));
  if (!Number.isFinite(observed) || !Number.isFinite(blockTime) || observed > now || blockTime > observed || now - observed > 60_000 || now - blockTime > 60_000) throw new Error('Network activation attestation is stale');
  const canonicalNetwork = { schemaVersion: 1, deploymentVersion: network.deploymentVersion, manifestHash: network.manifestHash, sourceManifestSha256: network.sourceManifestSha256, chainId: 125, publicRPC: network.publicRPC, explorerBase: network.explorerBase, native: { symbol: 'PAX', decimals: 18 }, settlement: { symbol: 'USDL', decimals: 6 }, addresses: Object.fromEntries(Object.entries(addresses).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)), creationFeeRaw: '1000000' };
  if (await sha(canonicalNetwork) !== serial.networkSha256) throw new Error('Canonical public network hash differs');
  const canonicalRevision = { schemaVersion: 2, deploymentVersion: revision.deploymentVersion, manifestSha256: revision.manifestSha256, capabilitySha256: revision.capabilitySha256, contractSupportSha256: revision.contractSupportSha256, serialInterfaceSha256: revision.serialInterfaceSha256, revisionSha256: '' };
  if (await sha(canonicalRevision) !== revision.revisionSha256) throw new Error('Canonical deployment revision differs');
  signal.throwIfAborted();
  return unknown as unknown as AuthorityEnvelope;
}

const QRCode = lazy(async () => {
  const { QRCodeSVG } = await import('qrcode.react');
  return { default: ({ value, label }: { value: string; label: string }) => createElement(QRCodeSVG, { value, title: label, 'aria-label': label, role: 'img', size: 192, level: 'M', marginSize: 4 }) };
});
function FundingQRCode(props: { value: string; label: string }) {
  return createElement(Suspense, { fallback: createElement('p', { role: 'status' }, 'Preparing funding QR code…') }, createElement(QRCode, props));
}

export async function loadRuntimeAuthority(signal: AbortSignal, privateFixtureProfile?: PrivateFixtureAuthorityProfileV1): Promise<RuntimeAuthority> {
  const fixture = privateFixtureProfile === undefined ? undefined : parsePrivateFixtureAuthorityProfileV1(privateFixtureProfile);
  const initial = await readAuthority(signal, fixture), { revision, network } = initial.serialExport;
  const pins: CodePin[] = ['Router', 'Quoter', 'PoolRegistry', 'EventEmitter'].map(role => {
    const identity = initial.attestation.identities[role]!;
    return { address: getAddress(identity.address), runtimeHash: identity.runtimeHash, ...(identity.implementationAddress ? { implementation: getAddress(identity.implementationAddress), implementationHash: identity.implementationHash! } : {}) };
  });
  const publicNetwork = Object.freeze({ ...network, addresses: Object.freeze({ ...network.addresses }), native: Object.freeze({ ...network.native }), settlement: Object.freeze({ ...network.settlement }) });
  const wallet: WalletNetworkAuthorityV2 = { network: publicNetwork, usdlRuntimeHash: initial.attestation.identities.USDL!.runtimeHash, FundingQRCode,
    async committedHead(headSignal) {
      const fresh = await readAuthority(headSignal, fixture);
      if (fresh.serialExport.revision.revisionSha256 !== revision.revisionSha256 || fresh.serialExport.networkSha256 !== initial.serialExport.networkSha256 || fresh.attestation.identities.USDL!.runtimeHash !== initial.attestation.identities.USDL!.runtimeHash) throw new Error('Network revision changed; refresh before continuing');
      return { number: BigInt(fresh.committedHead.number), hash: fresh.committedHead.hash };
    },
  };
  return { revision: Object.freeze(revision), expiresAt: Math.min(Date.parse(initial.attestation.observedAt), Date.parse(initial.attestation.blockTime)) + 60_000, network: wallet, tradeDeployment: { revision: revision.revisionSha256, router: getAddress(network.addresses.Router!), quoter: getAddress(network.addresses.Quoter!), registry: getAddress(network.addresses.PoolRegistry!), emitter: getAddress(network.addresses.EventEmitter!), usdl: getAddress(network.addresses.USDL!), pins },
    async preflight(family, request, requestSignal) {
      if (fixture) assertFixtureOrigin(fixture);
      const paths = { creation: 'create', plain_quote: 'quote', trading: 'trade', nft_fees: 'claim' } as const;
      const zero = '0x0000000000000000000000000000000000000000';
      const canonicalRequest = { family, expectedRevision: revision.revisionSha256, blockHash: request.blockHash.toLowerCase(), trader: getAddress(request.trader).toLowerCase(), recipient: getAddress(request.recipient).toLowerCase(), pool: getAddress(request.pool ?? zero).toLowerCase(), amountIn: request.amountIn ?? '', isBuy: request.isBuy ?? false, creationPath: request.creationPath ?? '', name: request.name ?? '', symbol: request.symbol ?? '', feeStrategy: request.feeStrategy ?? 0, optical: getAddress(request.optical ?? zero).toLowerCase(), nftId: request.nftId ?? '' };
      const response = object(await readJSON(`/v2/preflight/${paths[family]}`, requestSignal, canonicalRequest));
      const responseRevision = object(response.revision);
      if (response.schemaVersion !== 2 || response.family !== family || Object.entries(revision).some(([key, value]) => responseRevision[key] !== value) || response.blockHash !== canonicalRequest.blockHash || typeof response.trader !== 'string' || getAddress(response.trader) !== getAddress(request.trader) || typeof response.recipient !== 'string' || getAddress(response.recipient) !== getAddress(request.recipient) || !Number.isSafeInteger(response.blockNumber) || (response.blockNumber as number) < 0 || typeof response.supported !== 'boolean' || typeof response.executionPermitted !== 'boolean' || response.executionPermitted && !response.supported || response.requestSha256 !== await sha(canonicalRequest)) throw new Error('Preflight response differs from the reviewed context');
      return Object.freeze(response);
    },
  };
}

const RuntimeAuthorityContext = createContext<{ authority?: RuntimeAuthority; unavailableReason?: string }>({ unavailableReason: 'Getting ready, one moment' });
export function RuntimeAuthorityProvider({ children, privateFixtureProfile, privateFixtureProfileInput }: { children: ReactNode; privateFixtureProfile?: PrivateFixtureAuthorityProfileV1; privateFixtureProfileInput?: string }) {
  const [state, setState] = useState<{ authority?: RuntimeAuthority; unavailableReason?: string }>({ unavailableReason: 'Getting ready, one moment' });
  useEffect(() => {
    const controller = new AbortController(); let inFlight = false;
    let expiryTimer: ReturnType<typeof setTimeout> | undefined;
    const refresh = async () => {
      if (inFlight) return; inFlight = true;
      try {
        if (privateFixtureProfile !== undefined && privateFixtureProfileInput !== undefined) throw new Error('Approved network input is ambiguous');
        const fixture = privateFixtureProfileInput === undefined ? privateFixtureProfile : parsePrivateFixtureAuthorityProfileV1(privateFixtureProfileInput);
        const authority = await loadRuntimeAuthority(controller.signal, fixture);
        if (!controller.signal.aborted) {
          clearTimeout(expiryTimer);
          const remaining = authority.expiresAt - Date.now();
          if (remaining <= 0) throw new Error('Network activation attestation is stale');
          setState(previous => {
            const current = previous.authority;
            if (current && current.revision.revisionSha256 === authority.revision.revisionSha256 && JSON.stringify(current.tradeDeployment) === JSON.stringify(authority.tradeDeployment) && current.network.usdlRuntimeHash === authority.network.usdlRuntimeHash && JSON.stringify(current.network.network) === JSON.stringify(authority.network.network)) return { authority: { ...authority, network: current.network, tradeDeployment: current.tradeDeployment } };
            return { authority };
          });
          expiryTimer = setTimeout(() => { setState({ unavailableReason: 'Reconnecting, try again in a moment' }); void refresh(); }, remaining);
        }
      }
      catch (error) { if (!controller.signal.aborted) setState({ unavailableReason: reportUserFacingError(error, 'Not connecting right now, try again in a moment', 'network') }); }
      finally { inFlight = false; }
    };
    void refresh(); const timer = setInterval(() => { void refresh(); }, 30_000);
    const visible = () => { if (document.visibilityState === 'visible') { setState(previous => previous.authority && previous.authority.expiresAt <= Date.now() ? { unavailableReason: 'Reconnecting, try again in a moment' } : previous); void refresh(); } };
    document.addEventListener('visibilitychange', visible); window.addEventListener('online', refresh);
    return () => { controller.abort(); clearInterval(timer); clearTimeout(expiryTimer); document.removeEventListener('visibilitychange', visible); window.removeEventListener('online', refresh); };
  }, [privateFixtureProfile, privateFixtureProfileInput]);
  return createElement(RuntimeAuthorityContext.Provider, { value: state }, children);
}
export function useRuntimeAuthority() { return useContext(RuntimeAuthorityContext); }
