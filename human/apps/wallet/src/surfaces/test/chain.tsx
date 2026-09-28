import { readFileSync } from 'node:fs';
import path from 'node:path';
import type { ReactElement } from 'react';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { ethers } from 'ethers';
import {
    FEE_TOKEN_PRECOMPILE,
    announceProvider,
    decodeAbiString,
    type Eip1193Provider,
    type ProviderEvent,
    type ProviderListener,
} from '@paxeer/wallet';
import { abiSelector } from '@sidiora/layerx-sdk';
import { custodyChoiceRepository } from '@/platform/storage/repositories';
import { WalletProvider } from '@/wallet/WalletProvider';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

export const CHAIN_KEY = ethers.keccak256(ethers.toUtf8Bytes('paxeer-wallet-app-surfaces'));
export const CHAIN_UUID = '6c1f9a2e-4b7d-4e38-9a51-0d3c8e7f2b64';
export const GAS_LIMIT = 21_000n;
export const GAS_PRICE = 1_000_000_000n;

export interface SentCall {
    readonly from: string;
    readonly to: string;
    readonly data: string;
    readonly value: string;
}

export interface ChainLog {
    readonly address: string;
    readonly topics: string[];
    readonly data: string;
}

const SET_FEE_DENOM = abiSelector('setFeeDenom(string)');
const CLEAR_FEE_DENOM = abiSelector('clearFeeDenom()');
const GET_FEE_DENOM = abiSelector('getFeeDenom(address)');
const XWEB_FEE = abiSelector('fee()');

function word(value: bigint): string {
    return value.toString(16).padStart(64, '0');
}

export function abiString(value: string): string {
    const body = Buffer.from(value, 'utf8').toString('hex');
    return `0x${word(32n)}${word(BigInt(body.length / 2))}${body.padEnd(Math.ceil(body.length / 64) * 64, '0')}`;
}

export class SurfaceChain implements Eip1193Provider {
    readonly wallet = new ethers.Wallet(CHAIN_KEY);
    readonly address = this.wallet.address.toLowerCase();
    readonly methods: string[] = [];
    readonly sent: SentCall[] = [];
    readonly calls: Array<{ to: string; data: string }> = [];
    feeDenom = '';
    xwebFee = 0n;
    logsFor: (call: SentCall) => ChainLog[] = () => [];
    private readonly receipts = new Map<string, { status: string; logs: ChainLog[] }>();
    private readonly listeners = new Map<ProviderEvent, Set<ProviderListener>>();

    async request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown> {
        const params = Array.isArray(args.params) ? (args.params as readonly unknown[]) : [];
        this.methods.push(args.method);
        switch (args.method) {
            case 'eth_requestAccounts':
            case 'eth_accounts':
                return [this.wallet.address];
            case 'eth_chainId':
                return ethers.toQuantity(125);
            case 'eth_gasPrice':
                return ethers.toQuantity(GAS_PRICE);
            case 'eth_estimateGas':
                return ethers.toQuantity(GAS_LIMIT);
            case 'eth_call': {
                const call = params[0] as { to: string; data: string };
                this.calls.push({ to: call.to.toLowerCase(), data: call.data.toLowerCase() });
                return this.answer(call.to.toLowerCase(), call.data.toLowerCase());
            }
            case 'eth_sendTransaction': {
                const tx = params[0] as SentCall;
                if (tx.from.toLowerCase() !== this.address) throw Object.assign(new Error('unknown account'), { code: 4100 });
                const call = { from: tx.from.toLowerCase(), to: tx.to.toLowerCase(), data: tx.data.toLowerCase(), value: tx.value };
                this.sent.push(call);
                this.apply(call);
                const hash = ethers.id(`${this.sent.length}:${call.to}:${call.data}:${call.value}`);
                this.receipts.set(hash, { status: '0x1', logs: this.logsFor(call) });
                return hash;
            }
            case 'eth_getTransactionReceipt': {
                const receipt = this.receipts.get(String(params[0]));
                return receipt === undefined ? null : { transactionHash: params[0], ...receipt };
            }
            default:
                throw Object.assign(new Error(`unsupported method ${args.method}`), { code: 4200 });
        }
    }

    on(event: ProviderEvent, listener: ProviderListener): this {
        const set = this.listeners.get(event) ?? new Set<ProviderListener>();
        set.add(listener);
        this.listeners.set(event, set);
        return this;
    }

    removeListener(event: ProviderEvent, listener: ProviderListener): this {
        this.listeners.get(event)?.delete(listener);
        return this;
    }

    private answer(to: string, data: string): string {
        if (to === FEE_TOKEN_PRECOMPILE && data.startsWith(GET_FEE_DENOM)) return abiString(this.feeDenom);
        if (data === XWEB_FEE) return `0x${word(this.xwebFee)}`;
        throw Object.assign(new Error(`no contract answers ${data.slice(0, 10)} at ${to}`), { code: -32000 });
    }

    private apply(call: SentCall): void {
        if (call.to !== FEE_TOKEN_PRECOMPILE) return;
        if (call.data.startsWith(SET_FEE_DENOM)) this.feeDenom = decodeAbiString(`0x${call.data.slice(10)}`);
        if (call.data === CLEAR_FEE_DENOM) this.feeDenom = '';
    }
}

export interface EventSpecInput {
    readonly name: string;
    readonly type: string;
    readonly indexed: boolean;
}

export interface EventSpec {
    readonly name: string;
    readonly inputs: readonly EventSpecInput[];
}

export interface EventVector {
    readonly event: string;
    readonly fields: Record<string, string | boolean>;
}

const SDK_FIXTURES = path.resolve(__dirname, '../../../../../wallet/sdk/test/fixtures/modules');

export function eventVectors(section: string): EventVector[] {
    const all = JSON.parse(readFileSync(path.join(SDK_FIXTURES, 'events.json'), 'utf8')) as Record<string, EventVector[]>;
    return all[section] ?? [];
}

export function callFixtures(): Record<string, Record<string, unknown>> {
    return JSON.parse(readFileSync(path.join(SDK_FIXTURES, 'calls.json'), 'utf8')) as Record<string, Record<string, unknown>>;
}

function fieldWord(type: string, value: string | boolean): string {
    if (type === 'address') return String(value).slice(2).toLowerCase().padStart(64, '0');
    if (type === 'bytes32') return String(value).slice(2).toLowerCase();
    if (type === 'bool') return word(value ? 1n : 0n);
    return word(BigInt(value as string));
}

function tail(bytes: Buffer): string {
    const body = bytes.toString('hex');
    return word(BigInt(bytes.length)) + body.padEnd(Math.ceil(body.length / 64) * 64, '0');
}

export function encodeLog(address: string, spec: EventSpec, fields: Record<string, string | boolean>): ChainLog {
    const topics = [ethers.id(`${spec.name}(${spec.inputs.map((input) => input.type).join(',')})`)];
    const body = spec.inputs.filter((input) => !input.indexed);
    const heads: string[] = [];
    const tails: string[] = [];
    let offset = body.length * 32;
    for (const input of spec.inputs) {
        const value = fields[input.name];
        if (value === undefined) throw new Error(`the vector misses ${input.name}`);
        if (input.indexed) {
            topics.push(`0x${fieldWord(input.type, value)}`);
        } else if (input.type === 'string' || input.type === 'bytes') {
            const encoded = tail(input.type === 'string' ? Buffer.from(String(value), 'utf8') : Buffer.from(String(value).slice(2), 'hex'));
            heads.push(word(BigInt(offset)));
            tails.push(encoded);
            offset += encoded.length / 2;
        } else {
            heads.push(fieldWord(input.type, value));
        }
    }
    return { address: address.toLowerCase(), topics, data: `0x${heads.join('')}${tails.join('')}` };
}

export function vectorLog(address: string, specs: readonly EventSpec[], vector: EventVector): ChainLog {
    const spec = specs.find((candidate) => candidate.name === vector.event);
    if (!spec) throw new Error(`no event spec ${vector.event}`);
    return encodeLog(address, spec, vector.fields);
}

export function expectedField(spec: EventSpec, name: string, value: string | boolean): string {
    const input = spec.inputs.find((candidate) => candidate.name === name);
    if (!input) throw new Error(`no input ${name}`);
    if (input.type.startsWith('uint')) return BigInt(value as string).toString(10);
    if (typeof value === 'boolean') return String(value);
    return input.type === 'string' ? value : value.toLowerCase();
}

export interface Mounted {
    readonly container: HTMLDivElement;
    readonly unmount: () => Promise<void>;
}

export async function until(check: () => boolean, what: string): Promise<void> {
    for (let attempt = 0; attempt < 300; attempt += 1) {
        if (check()) return;
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 10));
        });
    }
    throw new Error(`condition not reached: ${what}`);
}

export async function mountSurface(chain: SurfaceChain, element: ReactElement): Promise<Mounted> {
    window.localStorage.clear();
    custodyChoiceRepository.write('injected');
    const target = new EventTarget();
    const stop = announceProvider(
        { info: { uuid: CHAIN_UUID, name: 'Surface Test Wallet', icon: 'data:image/svg+xml;base64,PHN2Zy8+', rdns: 'test.example.surfaces' }, provider: chain },
        target,
    );
    const container = document.createElement('div');
    document.body.appendChild(container);
    let root: Root | null = null;
    await act(async () => {
        root = createRoot(container);
        root.render(
            <WalletProvider config={null} identity={null} eventTarget={target}>
                {element}
            </WalletProvider>,
        );
    });
    await until(() => container.querySelector('[data-role="not-connected"]') === null && container.querySelector('section') !== null, 'wallet connected');
    return {
        container,
        unmount: async () => {
            await act(async () => {
                root?.unmount();
            });
            stop();
            container.remove();
        },
    };
}

export async function typeInto(container: HTMLElement, name: string, value: string): Promise<void> {
    const input = container.querySelector<HTMLInputElement>(`input[name="${name}"]`);
    if (!input) throw new Error(`no input ${name}`);
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
    if (!setter) throw new Error('no value setter');
    await act(async () => {
        setter.call(input, value);
        input.dispatchEvent(new Event('input', { bubbles: true }));
    });
}

export async function click(element: Element | null, what: string): Promise<void> {
    if (!(element instanceof HTMLElement)) throw new Error(`no element ${what}`);
    await act(async () => {
        element.click();
    });
}

export function radio(container: HTMLElement, group: string, label: string): Element | null {
    const node = container.querySelector(`[role="radiogroup"][aria-label="${group}"]`);
    return Array.from(node?.querySelectorAll('[role="radio"]') ?? []).find((option) => option.textContent === label) ?? null;
}

export function text(container: HTMLElement, selector: string): string {
    return container.querySelector(selector)?.textContent ?? '';
}
