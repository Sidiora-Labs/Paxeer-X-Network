// @vitest-environment jsdom
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import { readFeePreview } from './SendWidget';

const ADDRESS = `0x${'2a'.repeat(20)}`;
const BLOCK = {
    hash: `0x${'b1'.repeat(32)}`, parentHash: `0x${'b0'.repeat(32)}`, number: '0x10', timestamp: '0x6500', nonce: '0x0000000000000000',
    difficulty: '0x0', gasLimit: '0x1c9c380', gasUsed: '0x0', miner: `0x${'00'.repeat(20)}`, extraData: '0x', baseFeePerGas: '0x3b9aca00',
    transactions: [],
};

let server: Server;
let url: string;
let nonceTags: unknown[];
let failNonce: boolean;

beforeEach(async () => {
    window.localStorage.clear();
    nonceTags = [];
    failNonce = false;
    server = createServer((request, response) => {
        let body = '';
        request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
        request.on('end', () => {
            const parsed = JSON.parse(body) as { id: number; method: string; params: unknown[] } | { id: number; method: string; params: unknown[] }[];
            const answer = (call: { id: number; method: string; params: unknown[] }) => {
                switch (call.method) {
                    case 'eth_chainId': return { jsonrpc: '2.0', id: call.id, result: '0x7d' };
                    case 'eth_gasPrice': return { jsonrpc: '2.0', id: call.id, result: '0x3b9aca00' };
                    case 'eth_maxPriorityFeePerGas': return { jsonrpc: '2.0', id: call.id, result: '0x3b9aca00' };
                    case 'eth_getBlockByNumber': return { jsonrpc: '2.0', id: call.id, result: BLOCK };
                    case 'eth_getTransactionCount':
                        nonceTags.push(call.params[1]);
                        if (failNonce || call.params[1] === 'pending') return { jsonrpc: '2.0', id: call.id, error: { code: -32000, message: 'pending block is not available' } };
                        return { jsonrpc: '2.0', id: call.id, result: '0x7' };
                    default: return { jsonrpc: '2.0', id: call.id, error: { code: -32601, message: `method not found: ${call.method}` } };
                }
            };
            response.setHeader('content-type', 'application/json');
            response.end(JSON.stringify(Array.isArray(parsed) ? parsed.map(answer) : answer(parsed)));
        });
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterEach(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
});

describe('SendWidget fee preview', () => {
    it('reads the preview nonce at latest', async () => {
        const preview = await readFeePreview(new ethers.JsonRpcProvider(url, 125, { staticNetwork: true }), ADDRESS, null);
        expect(nonceTags).toEqual(['latest']);
        expect(preview.nonce).toBe(7);
        expect(preview.amountPax).not.toBe('');
    });

    it('rejects when the nonce read fails so the widget shows the preview as unavailable', async () => {
        failNonce = true;
        await expect(readFeePreview(new ethers.JsonRpcProvider(url, 125, { staticNetwork: true }), ADDRESS, `0x${'3b'.repeat(20)}`)).rejects.toThrow();
        expect(nonceTags).toEqual(['latest']);
    });
});
