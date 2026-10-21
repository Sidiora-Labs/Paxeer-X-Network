import { ethers } from 'ethers';
import {
    TOKENS, CONTRACTS, NATIVE_TOKEN, MAX_UINT256,
    PECOR_ABI, SIDIORA_ROUTER_ABI,
    executeSwap as pecorExecuteSwap,
    approveVault,
    buy as sidioraBuy,
    sell as sidioraSell,
    approveRouter as sidioraApproveRouter,
    quoteBuy as sidioraQuoteBuy,
    quoteSell as sidioraQuoteSell,
    quoteMultihop as sidioraQuoteMultihop,
} from '@/lib/swap/sdk';
import { toCentralSwapAddress, isSidioraToken } from './constants';
import type { SwapQuote, SwapParams } from './types';

// ── Approval helper (approves PECORRouter for ERC20 swaps) ─────────────────
export async function ensureApproval(
    signer: ethers.Signer,
    tokenAddress: string,
): Promise<boolean> {
    return approveVault(tokenAddress, signer, MAX_UINT256);
}

// ── Execute swap — dispatches by protocol ───────────────────────────────────
//
// Accepts any ethers.Signer so the same SDK drives `EmbeddedSigner`
// (Paxeer-managed, delegates `sendTransaction` to
// connect.paxportwallet.com) and `FundedSigner`. Read calls
// inside this module hit `signer.provider` regardless of custody model.
export async function executeSwap(
    signer: ethers.Signer,
    quote: SwapQuote,
    params: SwapParams,
): Promise<string> {
    switch (quote.protocol) {
        case 'pecor': return executePecorSwap(signer, quote, params);
        case 'sidiora': return executeSidioraSwap(signer, quote, params);
        case 'sidiora-multihop': return executeSidioraMultihop(signer, quote, params);
        case 'pecor-v4-multihop': return executeV4Multihop(signer, quote, params);
        default:
            throw new Error(`Unknown swap protocol: ${(quote as SwapQuote).protocol}`);
    }
}

// ── PECOR V3 ─────────────────────────────────────────────────────────────────

async function executePecorSwap(
    signer: ethers.Signer,
    quote: SwapQuote,
    params: SwapParams,
): Promise<string> {
    const tokenIn = toCentralSwapAddress(quote.tokenIn);
    const tokenOut = toCentralSwapAddress(quote.tokenOut);
    const amountIn = BigInt(quote.amountIn);
    const slipBps = params.slippageBps;
    const dl = BigInt(Math.floor(Date.now() / 1000) + (params.deadline ?? 300));
    const isNativeIn = tokenIn === NATIVE_TOKEN.address;
    const isNativeOut = tokenOut === NATIVE_TOKEN.address;
    const actualIn = isNativeIn ? NATIVE_TOKEN.wrappedAddress : tokenIn;
    const actualOut = isNativeOut ? NATIVE_TOKEN.wrappedAddress : tokenOut;

    const amountOutMin = (BigInt(quote.amountOut) * BigInt(10000 - slipBps)) / BigInt(10000);

    const pecor = new ethers.Contract(CONTRACTS.PECOR, PECOR_ABI, signer);
    let tx: ethers.TransactionResponse;
    if (isNativeIn) {
        // Native PAX → ERC20: PECOR contract handles payable native swaps
        tx = await pecor.swapExactInNative(actualOut, amountOutMin, dl, { value: amountIn });
    } else if (isNativeOut) {
        // ERC20 → Native PAX: PECOR contract handles native output
        await approveVault(tokenIn, signer, amountIn);
        tx = await pecor.swapExactInToNative(tokenIn, amountIn, amountOutMin, dl);
    } else {
        // ERC20 → ERC20: use PECOR.swapExactIn — same path as PECORQuoter.quoteExactIn
        await approveVault(tokenIn, signer, amountIn);
        tx = await pecor.swapExactIn(actualIn, actualOut, amountIn, amountOutMin, dl);
    }
    // Return hash immediately — CometBFT finalises in ~2s so no need to wait
    tx.wait().catch(() => { });
    return tx.hash;
}

// ── Sidiora direct buy/sell ───────────────────────────────────────────────

async function executeSidioraSwap(
    signer: ethers.Signer,
    quote: SwapQuote,
    params: SwapParams,
): Promise<string> {
    const pool = quote.poolAddress;
    if (!pool) throw new Error('Sidiora quote missing pool address');
    const dl = BigInt(Math.floor(Date.now() / 1000) + (params.deadline ?? 300));
    const amountIn = BigInt(quote.amountIn);
    const slipBps = params.slippageBps;
    const router = new ethers.Contract(CONTRACTS.Router, SIDIORA_ROUTER_ABI, signer);

    let tx: ethers.TransactionResponse;
    if (isSidioraToken(quote.tokenIn)) {
        // Sell: Sidiora token → USDL
        await sidioraApproveRouter(quote.tokenIn.address, signer);
        const q = await sidioraQuoteSell(pool, amountIn, signer.provider ?? undefined);
        const minOut = (q.amountOut * BigInt(10000 - slipBps)) / BigInt(10000);
        tx = await router.sell(pool, amountIn, minOut, dl);
    } else {
        // Buy: USDL → Launchpad
        await sidioraApproveRouter(TOKENS.USDL.address, signer);
        const q = await sidioraQuoteBuy(pool, amountIn, signer.provider ?? undefined);
        const minOut = (q.amountOut * BigInt(10000 - slipBps)) / BigInt(10000);
        tx = await router.buy(pool, amountIn, minOut, dl);
    }
    tx.wait().catch(() => { });
    return tx.hash;
}

// ── Sidiora token-to-token multihop ────────────────────────────────────────────

async function executeSidioraMultihop(
    signer: ethers.Signer,
    quote: SwapQuote,
    params: SwapParams,
): Promise<string> {
    const dl = BigInt(Math.floor(Date.now() / 1000) + (params.deadline ?? 300));
    const amountIn = BigInt(quote.amountIn);
    const slipBps = params.slippageBps;
    await sidioraApproveRouter(quote.tokenIn.address, signer);
    const q = await sidioraQuoteMultihop(quote.tokenIn.address, quote.tokenOut.address, amountIn, signer.provider ?? undefined);
    const minOut = (q.amountOut * BigInt(10000 - slipBps)) / BigInt(10000);
    const router = new ethers.Contract(CONTRACTS.Router, SIDIORA_ROUTER_ABI, signer);
    const tx = await router.swapTokenToToken(quote.tokenIn.address, quote.tokenOut.address, amountIn, minOut, dl);
    tx.wait().catch(() => { });
    return tx.hash;
}

// ── Cross-protocol 2-step swap: Sidiora ↔ PECOR ERC20 (via USDL bridge) ─────
// Executed as two sequential transactions. Each individual step is proven-working.
//   ERC20 → Sidiora: PECOR swap (ERC20→USDL)  then  Sidiora buy  (USDL→Token)
//   Sidiora → ERC20: Sidiora sell (Token→USDL) then  PECOR swap  (USDL→ERC20)

const TRANSFER_TOPIC = '0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef';

function parseUsdlReceived(receipt: ethers.TransactionReceipt, recipient: string): bigint {
    const toPadded = ('0x000000000000000000000000' + recipient.slice(2)).toLowerCase();
    const usdlAddr = TOKENS.USDL.address.toLowerCase();
    for (const log of receipt.logs) {
        if (
            log.address.toLowerCase() === usdlAddr &&
            log.topics[0]?.toLowerCase() === TRANSFER_TOPIC &&
            log.topics[2]?.toLowerCase() === toPadded
        ) {
            return BigInt(log.data);
        }
    }
    return BigInt(0);
}

async function executeV4Multihop(
    signer: ethers.Signer,
    quote: SwapQuote,
    params: SwapParams,
): Promise<string> {
    const amountIn = BigInt(quote.amountIn);
    const slipBps = params.slippageBps;
    const dl = params.deadline ?? 300;
    const pool = quote.poolAddress;
    if (!pool) throw new Error('V4 multihop quote missing pool address');

    const owner = await signer.getAddress();

    if (isSidioraToken(quote.tokenIn)) {
        // Step 1: Sidiora token → USDL
        const sellReceipt = await sidioraSell(
            pool, quote.tokenIn.address, amountIn, signer, slipBps, dl,
        );
        const usdlReceived = parseUsdlReceived(sellReceipt, owner);
        if (usdlReceived === BigInt(0)) throw new Error('Sidiora sell returned 0 USDL');

        // Step 2: USDL → ERC20
        const receipt = await pecorExecuteSwap(
            { tokenIn: TOKENS.USDL.address, tokenOut: toCentralSwapAddress(quote.tokenOut), amountIn: usdlReceived, slippageBps: slipBps, deadlineSeconds: dl },
            signer,
        );
        return receipt.hash;
    } else {
        // Step 1: ERC20 → USDL
        const pecorReceipt = await pecorExecuteSwap(
            { tokenIn: toCentralSwapAddress(quote.tokenIn), tokenOut: TOKENS.USDL.address, amountIn, slippageBps: slipBps, deadlineSeconds: dl },
            signer,
        );
        const usdlReceived = parseUsdlReceived(pecorReceipt, owner);
        if (usdlReceived === BigInt(0)) throw new Error('PECOR swap returned 0 USDL');

        // Step 2: USDL → Sidiora token
        const { receipt } = await sidioraBuy(
            pool, usdlReceived, signer, slipBps, dl,
        );
        return receipt.hash;
    }
}
