import { ethers } from 'ethers';
import type { Hex } from '@paxeer/wallet';

export const CUSTODY_PRECOMPILE = '0x0000000000000000000000000000000000001013' as const;
export const CUSTODY_HANDOFF_DOMAIN = 'LX:CUSTODY:v1';

export const CUSTODY_ABI = [
    'function deposit(bytes32 beneficiary) payable returns (bytes32 depositId)',
    'function depositToken(address pointer, uint256 amount, bytes32 beneficiary) returns (bytes32 depositId)',
] as const;

const custody = new ethers.Interface(CUSTODY_ABI);
const ACCOUNT_ID = /^[0-9a-f]{64}$/;

export class CustodyDepositError extends Error {
    constructor(message: string) {
        super(message);
        this.name = 'CustodyDepositError';
    }
}

export function beneficiaryOf(layerxAccount: string): Hex {
    const normalised = layerxAccount.toLowerCase();
    if (!ACCOUNT_ID.test(normalised) || /^0{64}$/.test(normalised)) {
        throw new CustodyDepositError('the main account id is not 32 bytes of hex');
    }
    return `0x${normalised}`;
}

export function depositTokenCalldata(pointer: string, amount: bigint, beneficiary: Hex): Hex {
    if (!ethers.isAddress(pointer)) throw new CustodyDepositError('the custody pointer is not an address');
    if (amount <= 0n) throw new CustodyDepositError('the deposit amount must be positive');
    return custody.encodeFunctionData('depositToken', [ethers.getAddress(pointer), amount, beneficiary]) as Hex;
}

export interface CustodyCall {
    readonly chainId: number;
    readonly to: string;
    readonly value: bigint;
    readonly data: Hex;
}

export function custodyHandoff(call: CustodyCall): Hex {
    const digest = ethers.keccak256(
        ethers.solidityPacked(['uint256', 'address', 'uint256', 'bytes'], [call.chainId, call.to, call.value, call.data]),
    );
    return ethers.hexlify(ethers.concat([ethers.toUtf8Bytes(CUSTODY_HANDOFF_DOMAIN), digest])) as Hex;
}
