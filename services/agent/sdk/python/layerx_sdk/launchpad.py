"""Calldata builders, wallet send helpers and event decoders for the Launchpad precompile.

Every write signature and event comes from precompiles/launchpad/abi.json through generated/precompile_writes.py."""

from __future__ import annotations

from collections.abc import Sequence
from typing import Literal

from .exchange import (
    DecodedEvent,
    EventInput,
    EventSpec,
    PrecompileAbiError,
    PrecompileCall,
    WalletRequest,
    decode_event_from,
    encode_signature_call,
    send_precompile_call,
)
from .generated.precompile_writes import LAUNCHPAD_EVENT_INPUTS, LAUNCHPAD_PRECOMPILE, LAUNCHPAD_WRITES

NO_OPTICAL = "0x0000000000000000000000000000000000000000"

LaunchpadNftWrite = Literal["claimAirdrop", "executeAirdrop", "executeBurn", "executeLpRewards"]

LAUNCHPAD_EVENTS: tuple[EventSpec, ...] = tuple(
    EventSpec(name, LAUNCHPAD_PRECOMPILE, tuple(EventInput(*item) for item in inputs))  # type: ignore[arg-type]
    for name, inputs in LAUNCHPAD_EVENT_INPUTS
)


def decode_launchpad_event(address: str, topics: Sequence[str], data: str) -> DecodedEvent:
    return decode_event_from(LAUNCHPAD_EVENTS, address, topics, data)


def launchpad_call(signature: str, values: Sequence[object], value: int = 0) -> PrecompileCall:
    """Any launchpad write a direct call to the precompile resolves, by canonical signature."""
    payable = LAUNCHPAD_WRITES.get(signature)
    if payable is None:
        raise PrecompileAbiError("invalid_value", f"{signature} is not a launchpad write")
    if not isinstance(value, int) or value < 0 or (value and not payable):
        raise PrecompileAbiError("invalid_value", f"{signature} value")
    return PrecompileCall(LAUNCHPAD_PRECOMPILE, encode_signature_call(signature, values), value)


def launchpad_buy_call(pool: str, usdl_in: int, min_tokens_out: int, deadline: int) -> PrecompileCall:
    """``buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline)``; the Router pays the caller."""
    return launchpad_call("buy(address,uint256,uint256,uint256)", [pool, usdl_in, min_tokens_out, deadline])


def launchpad_sell_call(pool: str, tokens_in: int, min_usdl_out: int, deadline: int) -> PrecompileCall:
    """``sell(address pool,uint256 tokenAmountIn,uint256 minUsdlOut,uint256 deadline)``."""
    return launchpad_call("sell(address,uint256,uint256,uint256)", [pool, tokens_in, min_usdl_out, deadline])


def launchpad_create_market_call(name: str, symbol: str, fee_strategy: int, optical: str = NO_OPTICAL) -> PrecompileCall:
    """``createMarket(string,string,uint8,address optical)``; the zero optical means none."""
    return launchpad_call("createMarket(string,string,uint8,address)", [name, symbol, fee_strategy, optical])


def launchpad_set_fee_strategy_call(nft_id: int, fee_strategy: int) -> PrecompileCall:
    """``setFeeStrategy(uint256 nftId,uint8 newStrategy)`` by the holder of the market's fee-rights NFT."""
    return launchpad_call("setFeeStrategy(uint256,uint8)", [nft_id, fee_strategy])


def launchpad_claim_fees_call(nft_id: int) -> PrecompileCall:
    """``claimFees(uint256 nftId)``; the fees go to the NFT holder."""
    return launchpad_call("claimFees(uint256)", [nft_id])


def launchpad_nft_call(write: LaunchpadNftWrite, nft_id: int) -> PrecompileCall:
    """``claimAirdrop``, ``executeAirdrop``, ``executeBurn`` or ``executeLpRewards`` on ``(uint256 nftId)``."""
    return launchpad_call(f"{write}(uint256)", [nft_id])


def send_launchpad_buy(request: WalletRequest, sender: str, pool: str, usdl_in: int, min_tokens_out: int, deadline: int) -> str:
    return send_precompile_call(request, sender, launchpad_buy_call(pool, usdl_in, min_tokens_out, deadline))


def send_launchpad_sell(request: WalletRequest, sender: str, pool: str, tokens_in: int, min_usdl_out: int, deadline: int) -> str:
    return send_precompile_call(request, sender, launchpad_sell_call(pool, tokens_in, min_usdl_out, deadline))


def send_launchpad_create_market(
    request: WalletRequest, sender: str, name: str, symbol: str, fee_strategy: int, optical: str = NO_OPTICAL
) -> str:
    return send_precompile_call(request, sender, launchpad_create_market_call(name, symbol, fee_strategy, optical))


def send_launchpad_set_fee_strategy(request: WalletRequest, sender: str, nft_id: int, fee_strategy: int) -> str:
    return send_precompile_call(request, sender, launchpad_set_fee_strategy_call(nft_id, fee_strategy))


def send_launchpad_claim_fees(request: WalletRequest, sender: str, nft_id: int) -> str:
    return send_precompile_call(request, sender, launchpad_claim_fees_call(nft_id))


def send_launchpad_nft_write(request: WalletRequest, sender: str, write: LaunchpadNftWrite, nft_id: int) -> str:
    return send_precompile_call(request, sender, launchpad_nft_call(write, nft_id))
