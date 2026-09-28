import { encodeFunctionData, type Hex } from 'viem';
import { publicClient } from './chainReads.js';

/**
 * HyperPaxeer network-native precompiles, surfaced to agents through the
 * wallet API so they don't have to hand-encode calldata.
 *
 * Addresses + ABIs verified against:
 *   knowledge/HyperPax-OS/precompiles/{scheduler,streams,eip712,teeattestor}/abi.json
 *   tools/paxeer/lib/config.mjs (PRECOMPILES map)
 *
 * Write builders return `{ to, data, value? }` ready to be threaded through the
 * agent policy engine + signer. Read helpers eth_call the precompile and decode
 * the typed result.
 */

export const PRECOMPILE_ADDRESSES = {
  orob: '0x0000000000000000000000000000000000000901',
  clearing: '0x0000000000000000000000000000000000000902',
  oracle: '0x0000000000000000000000000000000000000903',
  pofq: '0x0000000000000000000000000000000000000904',
  scheduler: '0x0000000000000000000000000000000000000905',
  streams: '0x0000000000000000000000000000000000000906',
  teeAttestor: '0x0000000000000000000000000000000000000907',
  eip712: '0x0000000000000000000000000000000000000908',
  staking: '0x0000000000000000000000000000000000000800',
  bech32: '0x0000000000000000000000000000000000000400',
  p256: '0x0000000000000000000000000000000000000100',
} as const satisfies Record<string, `0x${string}`>;

/** TEE attestation families — order matches x/attestor/types/keys.go + ITEEAttestor.sol. */
export const TEE_FAMILIES = {
  intel_tdx: 0,
  amd_sev_snp: 1,
  nvidia_h100: 2,
  intel_sgx: 3,
} as const;
export type TeeFamilyName = keyof typeof TEE_FAMILIES;

export interface BuiltCall {
  to: `0x${string}`;
  data: Hex;
  value?: string; // decimal wei string when payable
}

// -----------------------------------------------------------------------------
// ABIs
// -----------------------------------------------------------------------------

export const SCHEDULER_ABI = [
  {
    type: 'function',
    name: 'schedule',
    stateMutability: 'payable',
    inputs: [
      { name: 'target', type: 'address' },
      { name: 'callData', type: 'bytes' },
      { name: 'executeAtBlock', type: 'uint64' },
      { name: 'gasLimit', type: 'uint64' },
    ],
    outputs: [{ name: 'jobId', type: 'uint256' }],
  },
  { type: 'function', name: 'cancel', stateMutability: 'nonpayable', inputs: [{ name: 'jobId', type: 'uint256' }], outputs: [] },
  {
    type: 'function',
    name: 'reschedule',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'jobId', type: 'uint256' },
      { name: 'newBlock', type: 'uint64' },
    ],
    outputs: [],
  },
  {
    type: 'function',
    name: 'getJob',
    stateMutability: 'view',
    inputs: [{ name: 'jobId', type: 'uint256' }],
    outputs: [
      {
        name: 'job',
        type: 'tuple',
        components: [
          { name: 'id', type: 'uint256' },
          { name: 'creator', type: 'address' },
          { name: 'target', type: 'address' },
          { name: 'callData', type: 'bytes' },
          { name: 'executeAtBlock', type: 'uint64' },
          { name: 'gasLimit', type: 'uint64' },
          { name: 'deposit', type: 'uint256' },
          { name: 'active', type: 'bool' },
        ],
      },
    ],
  },
  { type: 'function', name: 'pending', stateMutability: 'view', inputs: [{ name: 'creator', type: 'address' }], outputs: [{ name: '', type: 'uint256[]' }] },
] as const;

export const STREAMS_ABI = [
  {
    type: 'function',
    name: 'open',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'payee', type: 'address' },
      { name: 'token', type: 'address' },
      { name: 'ratePerSecond', type: 'uint256' },
      { name: 'startTime', type: 'uint64' },
      { name: 'stopTime', type: 'uint64' },
      { name: 'cap', type: 'uint256' },
    ],
    outputs: [{ name: 'streamId', type: 'uint256' }],
  },
  { type: 'function', name: 'settle', stateMutability: 'nonpayable', inputs: [{ name: 'streamId', type: 'uint256' }], outputs: [{ name: 'paid', type: 'uint256' }] },
  { type: 'function', name: 'close', stateMutability: 'nonpayable', inputs: [{ name: 'streamId', type: 'uint256' }], outputs: [{ name: 'finalPaid', type: 'uint256' }] },
  {
    type: 'function',
    name: 'updateRate',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'streamId', type: 'uint256' },
      { name: 'newRate', type: 'uint256' },
    ],
    outputs: [],
  },
  { type: 'function', name: 'accrued', stateMutability: 'view', inputs: [{ name: 'streamId', type: 'uint256' }], outputs: [{ name: 'amount', type: 'uint256' }] },
  {
    type: 'function',
    name: 'getStream',
    stateMutability: 'view',
    inputs: [{ name: 'streamId', type: 'uint256' }],
    outputs: [
      {
        name: 'stream',
        type: 'tuple',
        components: [
          { name: 'id', type: 'uint256' },
          { name: 'payer', type: 'address' },
          { name: 'payee', type: 'address' },
          { name: 'token', type: 'address' },
          { name: 'ratePerSecond', type: 'uint256' },
          { name: 'cap', type: 'uint256' },
          { name: 'startTime', type: 'uint64' },
          { name: 'stopTime', type: 'uint64' },
          { name: 'settled', type: 'uint256' },
          { name: 'active', type: 'bool' },
        ],
      },
    ],
  },
] as const;

export const EIP712_ABI = [
  {
    type: 'function',
    name: 'hashTypedData',
    stateMutability: 'pure',
    inputs: [
      { name: 'domainSeparator', type: 'bytes32' },
      { name: 'structHash', type: 'bytes32' },
    ],
    outputs: [{ name: 'digest', type: 'bytes32' }],
  },
  {
    type: 'function',
    name: 'domainSeparator',
    stateMutability: 'pure',
    inputs: [
      { name: 'name', type: 'string' },
      { name: 'version', type: 'string' },
      { name: 'chainId', type: 'uint256' },
      { name: 'verifyingContract', type: 'address' },
    ],
    outputs: [{ name: 'separator', type: 'bytes32' }],
  },
  {
    type: 'function',
    name: 'recoverTypedSigner',
    stateMutability: 'view',
    inputs: [
      { name: 'domainSeparator', type: 'bytes32' },
      { name: 'structHash', type: 'bytes32' },
      { name: 'signature', type: 'bytes' },
    ],
    outputs: [{ name: 'signer', type: 'address' }],
  },
] as const;

export const TEE_ATTESTOR_ABI = [
  {
    type: 'function',
    name: 'verify',
    stateMutability: 'view',
    inputs: [
      { name: 'family', type: 'uint8' },
      { name: 'quote', type: 'bytes' },
    ],
    outputs: [
      {
        name: 'att',
        type: 'tuple',
        components: [
          { name: 'family', type: 'uint8' },
          { name: 'mrtd', type: 'bytes32' },
          { name: 'reportData', type: 'bytes32' },
          { name: 'timestamp', type: 'uint64' },
          { name: 'debug', type: 'bool' },
        ],
      },
    ],
  },
  {
    type: 'function',
    name: 'verifyAndExpect',
    stateMutability: 'view',
    inputs: [
      { name: 'family', type: 'uint8' },
      { name: 'quote', type: 'bytes' },
      { name: 'expectedReportData', type: 'bytes32' },
    ],
    outputs: [
      {
        name: 'att',
        type: 'tuple',
        components: [
          { name: 'family', type: 'uint8' },
          { name: 'mrtd', type: 'bytes32' },
          { name: 'reportData', type: 'bytes32' },
          { name: 'timestamp', type: 'uint64' },
          { name: 'debug', type: 'bool' },
        ],
      },
    ],
  },
  { type: 'function', name: 'rootOf', stateMutability: 'view', inputs: [{ name: 'family', type: 'uint8' }, { name: 'index', type: 'uint256' }], outputs: [{ name: 'root', type: 'bytes' }] },
  { type: 'function', name: 'rootCount', stateMutability: 'view', inputs: [{ name: 'family', type: 'uint8' }], outputs: [{ name: 'count', type: 'uint256' }] },
] as const;

// -----------------------------------------------------------------------------
// Scheduler (0x0905)
// -----------------------------------------------------------------------------

export const scheduler = {
  schedule(args: {
    target: `0x${string}`;
    callData?: Hex;
    executeAtBlock: bigint;
    gasLimit: bigint;
    depositWei?: bigint;
  }): BuiltCall {
    const data = encodeFunctionData({
      abi: SCHEDULER_ABI,
      functionName: 'schedule',
      args: [args.target, args.callData ?? '0x', args.executeAtBlock, args.gasLimit],
    });
    return {
      to: PRECOMPILE_ADDRESSES.scheduler,
      data,
      value: args.depositWei !== undefined ? args.depositWei.toString() : undefined,
    };
  },
  cancel(jobId: bigint): BuiltCall {
    return {
      to: PRECOMPILE_ADDRESSES.scheduler,
      data: encodeFunctionData({ abi: SCHEDULER_ABI, functionName: 'cancel', args: [jobId] }),
    };
  },
  reschedule(jobId: bigint, newBlock: bigint): BuiltCall {
    return {
      to: PRECOMPILE_ADDRESSES.scheduler,
      data: encodeFunctionData({ abi: SCHEDULER_ABI, functionName: 'reschedule', args: [jobId, newBlock] }),
    };
  },
  async getJob(jobId: bigint): Promise<{
    id: string;
    creator: string;
    target: string;
    callData: string;
    executeAtBlock: string;
    gasLimit: string;
    deposit: string;
    active: boolean;
  } | null> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.scheduler,
      abi: SCHEDULER_ABI,
      functionName: 'getJob',
      args: [jobId],
    })) as {
      id: bigint;
      creator: string;
      target: string;
      callData: string;
      executeAtBlock: bigint;
      gasLimit: bigint;
      deposit: bigint;
      active: boolean;
    };
    if (r.id === 0n && !r.active) return null;
    return {
      id: r.id.toString(),
      creator: r.creator,
      target: r.target,
      callData: r.callData,
      executeAtBlock: r.executeAtBlock.toString(),
      gasLimit: r.gasLimit.toString(),
      deposit: r.deposit.toString(),
      active: r.active,
    };
  },
  async pending(creator: `0x${string}`): Promise<string[]> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.scheduler,
      abi: SCHEDULER_ABI,
      functionName: 'pending',
      args: [creator],
    })) as readonly bigint[];
    return r.map((x) => x.toString());
  },
};

// -----------------------------------------------------------------------------
// PaymentStreams (0x0906)
// -----------------------------------------------------------------------------

export const streams = {
  open(args: {
    payee: `0x${string}`;
    token: `0x${string}`;
    ratePerSecond: bigint;
    startTime?: bigint;
    stopTime?: bigint;
    cap: bigint;
  }): BuiltCall {
    const data = encodeFunctionData({
      abi: STREAMS_ABI,
      functionName: 'open',
      args: [args.payee, args.token, args.ratePerSecond, args.startTime ?? 0n, args.stopTime ?? 0n, args.cap],
    });
    return { to: PRECOMPILE_ADDRESSES.streams, data };
  },
  settle(streamId: bigint): BuiltCall {
    return {
      to: PRECOMPILE_ADDRESSES.streams,
      data: encodeFunctionData({ abi: STREAMS_ABI, functionName: 'settle', args: [streamId] }),
    };
  },
  close(streamId: bigint): BuiltCall {
    return {
      to: PRECOMPILE_ADDRESSES.streams,
      data: encodeFunctionData({ abi: STREAMS_ABI, functionName: 'close', args: [streamId] }),
    };
  },
  updateRate(streamId: bigint, newRate: bigint): BuiltCall {
    return {
      to: PRECOMPILE_ADDRESSES.streams,
      data: encodeFunctionData({ abi: STREAMS_ABI, functionName: 'updateRate', args: [streamId, newRate] }),
    };
  },
  async accrued(streamId: bigint): Promise<string> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.streams,
      abi: STREAMS_ABI,
      functionName: 'accrued',
      args: [streamId],
    })) as bigint;
    return r.toString();
  },
  async getStream(streamId: bigint): Promise<Record<string, string | boolean> | null> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.streams,
      abi: STREAMS_ABI,
      functionName: 'getStream',
      args: [streamId],
    })) as {
      id: bigint;
      payer: string;
      payee: string;
      token: string;
      ratePerSecond: bigint;
      cap: bigint;
      startTime: bigint;
      stopTime: bigint;
      settled: bigint;
      active: boolean;
    };
    if (r.id === 0n && !r.active) return null;
    return {
      id: r.id.toString(),
      payer: r.payer,
      payee: r.payee,
      token: r.token,
      ratePerSecond: r.ratePerSecond.toString(),
      cap: r.cap.toString(),
      startTime: r.startTime.toString(),
      stopTime: r.stopTime.toString(),
      settled: r.settled.toString(),
      active: r.active,
    };
  },
};

// -----------------------------------------------------------------------------
// EIP-712 helper (0x0908) — all pure/view
// -----------------------------------------------------------------------------

export const eip712 = {
  async hashTypedData(domainSeparator: Hex, structHash: Hex): Promise<Hex> {
    return (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.eip712,
      abi: EIP712_ABI,
      functionName: 'hashTypedData',
      args: [domainSeparator, structHash],
    })) as Hex;
  },
  async domainSeparator(args: {
    name: string;
    version: string;
    chainId: bigint;
    verifyingContract: `0x${string}`;
  }): Promise<Hex> {
    return (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.eip712,
      abi: EIP712_ABI,
      functionName: 'domainSeparator',
      args: [args.name, args.version, args.chainId, args.verifyingContract],
    })) as Hex;
  },
  async recoverTypedSigner(domainSeparator: Hex, structHash: Hex, signature: Hex): Promise<Hex> {
    return (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.eip712,
      abi: EIP712_ABI,
      functionName: 'recoverTypedSigner',
      args: [domainSeparator, structHash, signature],
    })) as Hex;
  },
};

// -----------------------------------------------------------------------------
// TEE Attestor (0x0907) — all view
// -----------------------------------------------------------------------------

export interface Attestation {
  family: number;
  family_name: TeeFamilyName | 'unknown';
  mrtd: Hex;
  report_data: Hex;
  timestamp: string;
  debug: boolean;
}

function familyName(f: number): TeeFamilyName | 'unknown' {
  const hit = (Object.entries(TEE_FAMILIES) as [TeeFamilyName, number][]).find(([, v]) => v === f);
  return hit ? hit[0] : 'unknown';
}

function shapeAttestation(r: {
  family: number;
  mrtd: Hex;
  reportData: Hex;
  timestamp: bigint;
  debug: boolean;
}): Attestation {
  return {
    family: r.family,
    family_name: familyName(r.family),
    mrtd: r.mrtd,
    report_data: r.reportData,
    timestamp: r.timestamp.toString(),
    debug: r.debug,
  };
}

export const teeAttestor = {
  async verify(family: number, quote: Hex): Promise<Attestation> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.teeAttestor,
      abi: TEE_ATTESTOR_ABI,
      functionName: 'verify',
      args: [family, quote],
    })) as { family: number; mrtd: Hex; reportData: Hex; timestamp: bigint; debug: boolean };
    return shapeAttestation(r);
  },
  async verifyAndExpect(family: number, quote: Hex, expectedReportData: Hex): Promise<Attestation> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.teeAttestor,
      abi: TEE_ATTESTOR_ABI,
      functionName: 'verifyAndExpect',
      args: [family, quote, expectedReportData],
    })) as { family: number; mrtd: Hex; reportData: Hex; timestamp: bigint; debug: boolean };
    return shapeAttestation(r);
  },
  async rootOf(family: number, index: bigint): Promise<Hex> {
    return (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.teeAttestor,
      abi: TEE_ATTESTOR_ABI,
      functionName: 'rootOf',
      args: [family, index],
    })) as Hex;
  },
  async rootCount(family: number): Promise<string> {
    const r = (await publicClient().readContract({
      address: PRECOMPILE_ADDRESSES.teeAttestor,
      abi: TEE_ATTESTOR_ABI,
      functionName: 'rootCount',
      args: [family],
    })) as bigint;
    return r.toString();
  },
};
