/**
 * PNS — Paxeer Name Service library.
 *
 * ENS-compatible contracts deployed on Paxeer (chain 125).
 * TLD: .pax
 *
 * Combines on-chain contract calls (registration, renewal, transfer, set-primary)
 * with the BENS indexer API for read queries (owned names, domain detail, events).
 */

import { ethers } from 'ethers';
import { PAXEER_CONFIG, getActiveRpcUrl } from '@/lib/constants';

// ── Contract addresses ──────────────────────────────────────────────────────

export const PNS_CONTRACTS = {
    ethController: '0xed04f2968c6307f193abcfffd84e6dbd0849d0d9',
    publicResolver: '0x0f7b84376b9c474b4ae92783b5cf816ce3b17e64',
    reverseRegistrar: '0x967e8e4545530a37d687585d7ab39fa48c423761',
} as const;

export const PNS_TLD = 'pax';
export const PNS_CHAIN_ID = PAXEER_CONFIG.chainId;
export const REGISTRATION_DURATION_1Y = 365 * 24 * 60 * 60;

// BENS indexer base
const BENS_API = (process.env.NEXT_PUBLIC_PNS_API_BASE ?? '').replace(/\/+$/, '');

// ── ABIs (minimal, only what we need) ───────────────────────────────────────

const registrationTuple = {
    name: 'registration',
    type: 'tuple',
    components: [
        { name: 'label', type: 'string' },
        { name: 'owner', type: 'address' },
        { name: 'duration', type: 'uint256' },
        { name: 'secret', type: 'bytes32' },
        { name: 'resolver', type: 'address' },
        { name: 'data', type: 'bytes[]' },
        { name: 'reverseRecord', type: 'uint8' },
        { name: 'referrer', type: 'bytes32' },
    ],
} as const;

const controllerAbi = [
    {
        name: 'rentPrice',
        type: 'function',
        stateMutability: 'view',
        inputs: [{ name: 'label', type: 'string' }, { name: 'duration', type: 'uint256' }],
        outputs: [{ name: 'base', type: 'uint256' }, { name: 'premium', type: 'uint256' }],
    },
    {
        name: 'available',
        type: 'function',
        stateMutability: 'view',
        inputs: [{ name: 'label', type: 'string' }],
        outputs: [{ name: '', type: 'bool' }],
    },
    {
        name: 'makeCommitment',
        type: 'function',
        stateMutability: 'pure',
        inputs: [registrationTuple],
        outputs: [{ name: '', type: 'bytes32' }],
    },
    {
        name: 'commit',
        type: 'function',
        stateMutability: 'nonpayable',
        inputs: [{ name: 'commitment', type: 'bytes32' }],
        outputs: [],
    },
    {
        name: 'register',
        type: 'function',
        stateMutability: 'payable',
        inputs: [registrationTuple],
        outputs: [],
    },
    {
        name: 'minCommitmentAge',
        type: 'function',
        stateMutability: 'view',
        inputs: [],
        outputs: [{ name: '', type: 'uint256' }],
    },
    {
        name: 'renew',
        type: 'function',
        stateMutability: 'payable',
        inputs: [{ name: 'label', type: 'string' }, { name: 'duration', type: 'uint256' }],
        outputs: [],
    },
];

const resolverAbi = [
    'function setAddr(bytes32 node, address a)',
    'function addr(bytes32 node) view returns (address)',
    'function name(bytes32 node) view returns (string)',
];

const reverseRegistrarAbi = [
    'function setName(string name) returns (bytes32)',
];

const erc721Abi = [
    'function ownerOf(uint256 tokenId) view returns (address)',
    'function safeTransferFrom(address from, address to, uint256 tokenId)',
    'function transferFrom(address from, address to, uint256 tokenId)',
];

// ── Contract helpers ────────────────────────────────────────────────────────

function getProvider() {
    return new ethers.JsonRpcProvider(getActiveRpcUrl());
}

function getController(signerOrProvider?: ethers.Signer | ethers.Provider) {
    return new ethers.Contract(
        PNS_CONTRACTS.ethController,
        controllerAbi,
        signerOrProvider || getProvider(),
    );
}

export function labelToTokenId(label: string): bigint {
    return BigInt(ethers.keccak256(ethers.toUtf8Bytes(label)));
}

function randomSecret(): string {
    return ethers.keccak256(ethers.toUtf8Bytes(Date.now().toString() + Math.random().toString()));
}

// ── BENS API types ──────────────────────────────────────────────────────────

export interface PNSAddress {
    hash: string;
    ens_domain_name?: string;
}

export interface PNSDomain {
    id: string;
    name: string;
    resolved_address?: PNSAddress;
    owner?: PNSAddress;
    wrapped_owner?: PNSAddress;
    registration_date?: string;
    expiry_date?: string;
    protocol?: PNSProtocol;
}

export interface PNSDetailedDomain extends PNSDomain {
    tokens?: { id: string; contract_hash: string; type: string }[];
    registrant?: PNSAddress;
    other_addresses?: Record<string, string>;
    stored_offchain?: boolean;
    resolver_address?: PNSAddress;
}

export interface PNSDomainEvent {
    transaction_hash: string;
    timestamp: string;
    from_address?: PNSAddress;
    action?: string;
}

export interface PNSProtocol {
    id: string;
    short_name: string;
    title: string;
    description?: string;
    tld_list?: string[];
    icon_url?: string;
}

// ── BENS API fetch functions ────────────────────────────────────────────────

export async function fetchOwnedDomains(address: string): Promise<PNSDomain[]> {
    if (!BENS_API) return [];
    try {
        const params = new URLSearchParams({
            address,
            owned_by: 'true',
            only_active: 'true',
            sort: 'registration_date',
            order: 'DESC',
        });
        const res = await fetch(`${BENS_API}/api/v1/addresses:lookup?${params}`);
        if (!res.ok) return [];
        const data = await res.json();
        return data.items || [];
    } catch {
        return [];
    }
}

export async function fetchDomainDetail(name: string): Promise<PNSDetailedDomain | null> {
    if (!BENS_API) return null;
    try {
        const res = await fetch(`${BENS_API}/api/v1/domains/${encodeURIComponent(name)}`);
        if (!res.ok) return null;
        return res.json();
    } catch {
        return null;
    }
}

export async function fetchDomainEvents(name: string): Promise<PNSDomainEvent[]> {
    if (!BENS_API) return [];
    try {
        const res = await fetch(`${BENS_API}/api/v1/domains/${encodeURIComponent(name)}/events?order=DESC`);
        if (!res.ok) return [];
        const data = await res.json();
        return data.items || [];
    } catch {
        return [];
    }
}

export async function fetchAddressName(address: string): Promise<PNSDetailedDomain | null> {
    if (!BENS_API) return null;
    try {
        const res = await fetch(`${BENS_API}/api/v1/addresses/${address}`);
        if (!res.ok) return null;
        const data = await res.json();
        return data.domain || null;
    } catch {
        return null;
    }
}

export async function lookupDomain(name: string): Promise<PNSDomain[]> {
    if (!BENS_API) return [];
    try {
        const params = new URLSearchParams({
            name,
            only_active: 'true',
        });
        const res = await fetch(`${BENS_API}/api/v1/domains:lookup?${params}`);
        if (!res.ok) return [];
        const data = await res.json();
        return data.items || [];
    } catch {
        return [];
    }
}

// ── On-chain read functions ─────────────────────────────────────────────────

export interface PriceResult {
    base: bigint;
    premium: bigint;
    total: bigint;
}

export async function checkAvailability(label: string): Promise<boolean> {
    const controller = getController();
    return controller.available(label);
}

export async function getRentPrice(label: string, durationSeconds: number): Promise<PriceResult> {
    const controller = getController();
    const [base, premium] = await controller.rentPrice(label, BigInt(durationSeconds));
    return { base, premium, total: base + premium };
}

export async function getMinCommitmentAge(): Promise<number> {
    const controller = getController();
    const age = await controller.minCommitmentAge();
    return Number(age);
}

// ── Registration (commit-reveal) ────────────────────────────────────────────

export interface RegistrationParams {
    label: string;
    owner: string;
    durationSeconds: number;
}

export interface CommitResult {
    commitmentHash: string;
    secret: string;
    txHash: string;
    registration: RegistrationTuple;
}

export interface RegistrationTuple {
    label: string;
    owner: string;
    duration: bigint;
    secret: string;
    resolver: string;
    data: string[];
    reverseRecord: number;
    referrer: string;
    _array: any[];
}

export async function commitName(
    signer: ethers.Signer,
    params: RegistrationParams,
): Promise<CommitResult> {
    const controller = getController(signer);
    const secret = randomSecret();
    const node = ethers.namehash(`${params.label}.${PNS_TLD}`);
    const ZERO_BYTES32 = ethers.ZeroHash;

    const resolverIface = new ethers.Interface(resolverAbi);
    const addrData = resolverIface.encodeFunctionData('setAddr', [node, params.owner]);

    const regArray = [
        params.label,
        params.owner,
        BigInt(params.durationSeconds),
        secret,
        PNS_CONTRACTS.publicResolver,
        [addrData],
        1,
        ZERO_BYTES32,
    ];

    const registration: RegistrationTuple = {
        label: params.label,
        owner: params.owner,
        duration: BigInt(params.durationSeconds),
        secret,
        resolver: PNS_CONTRACTS.publicResolver,
        data: [addrData],
        reverseRecord: 1,
        referrer: ZERO_BYTES32,
        _array: regArray,
    };

    const commitmentHash = await controller.makeCommitment(regArray);

    const tx = await controller.commit(commitmentHash, { gasLimit: 200000 });
    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) throw new Error('Commit transaction reverted');

    return {
        commitmentHash,
        secret,
        txHash: tx.hash,
        registration,
    };
}

export async function registerName(
    signer: ethers.Signer,
    registration: RegistrationTuple,
    price: PriceResult,
): Promise<string> {
    const controller = getController(signer);
    const value = (price.total * BigInt(110)) / BigInt(100);

    const tx = await controller.register(registration._array, { value, gasLimit: 500000 });
    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) throw new Error('Register transaction reverted');
    return tx.hash;
}

// ── Renewal ─────────────────────────────────────────────────────────────────

export async function renewName(
    signer: ethers.Signer,
    label: string,
    durationSeconds: number,
): Promise<string> {
    const controller = getController(signer);
    const price = await getRentPrice(label, durationSeconds);
    const value = (price.total * BigInt(110)) / BigInt(100);

    const tx = await controller.renew(label, BigInt(durationSeconds), { value });
    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) throw new Error('Renew transaction reverted');
    return tx.hash;
}

// ── Transfer (ERC-721) ──────────────────────────────────────────────────────

export async function transferName(
    signer: ethers.Signer,
    domain: PNSDetailedDomain,
    to: string,
): Promise<string> {
    const token = domain.tokens?.find((t) => t.type === 'NATIVE_DOMAIN_TOKEN');
    if (!token) throw new Error('No transferable token found for this domain');

    const nft = new ethers.Contract(token.contract_hash, erc721Abi, signer);
    const from = await signer.getAddress();
    const tx = await nft.transferFrom(from, to, BigInt(token.id));
    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) throw new Error('Transfer transaction reverted');
    return tx.hash;
}

// ── Set primary name (reverse record) ───────────────────────────────────────

export async function setPrimaryName(
    signer: ethers.Signer,
    fullName: string,
): Promise<string> {
    const reverseReg = new ethers.Contract(PNS_CONTRACTS.reverseRegistrar, reverseRegistrarAbi, signer);
    const tx = await reverseReg.setName(fullName);
    const receipt = await tx.wait();
    if (!receipt || receipt.status !== 1) throw new Error('Set primary name transaction reverted');
    return tx.hash;
}

// ── Format helpers ──────────────────────────────────────────────────────────

export function formatPaxPrice(wei: bigint): string {
    const val = Number(wei) / 1e18;
    if (val < 0.001) return '< 0.001';
    if (val < 1) return val.toFixed(4);
    if (val < 100) return val.toFixed(3);
    return val.toFixed(2);
}

export function isValidLabel(label: string): boolean {
    if (!label || label.length < 3) return false;
    if (label.length > 64) return false;
    return /^[a-z0-9-]+$/.test(label);
}

export function formatExpiry(iso: string | undefined): string {
    if (!iso) return 'Never';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return 'Unknown';
    const now = Date.now();
    const diff = d.getTime() - now;
    if (diff < 0) return 'Expired';
    const days = Math.floor(diff / 86_400_000);
    if (days > 365) return `${Math.floor(days / 365)}y ${days % 365}d`;
    if (days > 30) return `${Math.floor(days / 30)}mo ${days % 30}d`;
    return `${days}d`;
}

export function shortenAddr(addr: string): string {
    if (!addr || addr.length < 10) return addr || '';
    return `${addr.slice(0, 6)}...${addr.slice(-4)}`;
}
