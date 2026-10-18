# File generated from our OpenAPI spec by Scalar. See README.md for details.

# Smoke test: calls every generated operation once to confirm the SDK can reach each endpoint.
# Run it from this repo with `python tests/smoke-test.py`. The generator also runs this file
# against a mock server and reads the JSON report produced via SCALAR_SMOKE_REPORT.
from __future__ import annotations

import json
import os
import sys
import time
import traceback
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any, Callable, TypedDict

from chainflow_data_gateway import ChainflowDataGateway

# The shared smoke-test runner injects base URL and credentials through the same
# environment variables the generated client reads in normal use.
client = ChainflowDataGateway(max_retries=2, timeout=10)


class SmokeResult(TypedDict, total=False):
    operation: str
    method: str
    path: str
    label: str
    status: str
    durationMs: int
    error: str


class _SmokeCaseBase(TypedDict):
    operation: str
    method: str
    path: str
    run: Callable[[], Any]


# `label` says which of an operation's two calls this is — "required params" or "all params".
# It sits in a total=False extension because it is absent when the operation contributed a
# single case, while the fields above are always present.
class SmokeCase(_SmokeCaseBase, total=False):
    label: str


def _smoke_case_0() -> None:
    gateway = client.gateway.list_health()


def _smoke_case_1() -> None:
    gateway = client.gateway.create_web_socket_token(
        symbol="btc",
    )


def _smoke_case_2() -> None:
    price_feed = client.price_feeds.list(
        market="btc",
    )


def _smoke_case_3() -> None:
    price_feed = client.price_feeds.list_health(
        market="btc",
    )


def _smoke_case_4() -> None:
    price_feed = client.price_feeds.list_spot_style_stats(
        market="btc",
    )


def _smoke_case_5() -> None:
    price_feed = client.price_feeds.list_perpetual_stats(
        market="btc",
    )


def _smoke_case_6() -> None:
    price_feed = client.price_feeds.list_trade_tape_status(
        market="btc",
    )


def _smoke_case_7() -> None:
    price_feed = client.price_feeds.retrieve_recent_spot_trades(
        market="btc",
        symbol="symbol",
        limit=100,
    )


def _smoke_case_8() -> None:
    price_feed = client.price_feeds.retrieve_recent_perpetual_trades(
        market="btc",
        symbol="symbol",
        limit=100,
    )


def _smoke_case_9() -> None:
    trading_view = client.trading_view.list_datafeed_config(
        market="btc",
    )


def _smoke_case_10() -> None:
    trading_view = client.trading_view.list_feed_time(
        market="btc",
    )


def _smoke_case_11() -> None:
    trading_view = client.trading_view.resolve_symbol(
        market="btc",
        symbol="BTC",
    )


def _smoke_case_12() -> None:
    trading_view = client.trading_view.search(
        market="btc",
        query="",
        limit=30,
    )


def _smoke_case_13() -> None:
    trading_view = client.trading_view.list_candle_history(
        market="btc",
        symbol="BTC",
        resolution="1",
        from_=1,
        to=1,
        countback=1000,
    )


def _smoke_case_14() -> None:
    trading_view = client.trading_view.list_quotes(
        market="btc",
    )


def _smoke_case_15() -> None:
    trading_view = client.trading_view.list_quotes(
        market="btc",
        symbols=["BTC"],
    )


def _smoke_case_16() -> None:
    market = client.markets.list_health()


def _smoke_case_17() -> None:
    market = client.markets.list()


def _smoke_case_18() -> None:
    market = client.markets.retrieve(
        symbol="BTC",
    )


def _smoke_case_19() -> None:
    market = client.markets.list_totals()


def _smoke_case_20() -> None:
    market = client.markets.list_tickers()


def _smoke_case_21() -> None:
    market = client.markets.retrieve_ticker(
        symbol="symbol",
    )


def _smoke_case_22() -> None:
    market = client.markets.list_open_interest()


def _smoke_case_23() -> None:
    explorer = client.explorer.list_health()


def _smoke_case_24() -> None:
    explorer = client.explorer.list_blocks(
        limit=20,
    )


def _smoke_case_25() -> None:
    explorer = client.explorer.list_transactions(
        limit=50,
    )


def _smoke_case_26() -> None:
    coin_gecko = client.coin_gecko.list(
        include_platform=False,
    )


def _smoke_case_27() -> None:
    coin_gecko = client.coin_gecko.list_markets(
        vs_currency="usd",
        page=1,
        per_page=100,
        order="market_cap_desc",
        sparkline=False,
        precision="full",
        locale="en",
    )


def _smoke_case_28() -> None:
    coin_gecko = client.coin_gecko.list_markets(
        vs_currency="usd",
        ids=["bitcoin"],
        symbols=["btc"],
        page=1,
        per_page=100,
        order="market_cap_desc",
        sparkline=False,
        price_change_percentage=["1h"],
        precision="full",
        locale="en",
    )


def _smoke_case_29() -> None:
    coin_gecko = client.coin_gecko.retrieve(
        id="bitcoin",
        localization=False,
        tickers=False,
        community_data=False,
        developer_data=False,
        sparkline=False,
        market_data=True,
    )


def _smoke_case_30() -> None:
    coin_gecko = client.coin_gecko.list_simple_prices(
        vs_currencies="usd",
        include_market_cap=False,
        include_24hr_vol=False,
        include_24hr_change=False,
        include_last_updated_at=False,
    )


def _smoke_case_31() -> None:
    coin_gecko = client.coin_gecko.list_simple_prices(
        ids=["bitcoin"],
        symbols=["btc"],
        vs_currencies="usd",
        include_market_cap=False,
        include_24hr_vol=False,
        include_24hr_change=False,
        include_last_updated_at=False,
    )


def _smoke_case_32() -> None:
    coin_market_cap = client.coin_market_cap.list_quotes(
        convert="USD",
    )


def _smoke_case_33() -> None:
    coin_market_cap = client.coin_market_cap.list_quotes(
        convert="USD",
        symbol=["BTC"],
        slug=["bitcoin"],
    )


def _smoke_case_34() -> None:
    coin_market_cap = client.coin_market_cap.list_listings(
        convert="USD",
        start=1,
        limit=100,
        sort="market_cap",
        sort_dir="desc",
    )


def _smoke_case_35() -> None:
    coin_market_cap = client.coin_market_cap.list_quotes_api_alias(
        convert="USD",
    )


def _smoke_case_36() -> None:
    coin_market_cap = client.coin_market_cap.list_quotes_api_alias(
        convert="USD",
        symbol=["BTC"],
        slug=["bitcoin"],
    )


def _smoke_case_37() -> None:
    coin_market_cap = client.coin_market_cap.list_listings_api_alias(
        convert="USD",
        start=1,
        limit=100,
        sort="market_cap",
        sort_dir="desc",
    )


def _smoke_case_38() -> None:
    def _probe() -> None:
        with client.web_sockets.connect_price_feed(
            market="btc",
        ) as socket:
            try:
                socket.recv()
            finally:
                socket.close(code=1000, reason="smoke-test")

    _probe()


def _smoke_case_39() -> None:
    def _probe() -> None:
        with client.web_sockets.connect_markets() as socket:
            try:
                socket.recv()
            finally:
                socket.close(code=1000, reason="smoke-test")

    _probe()


def _smoke_case_40() -> None:
    def _probe() -> None:
        with client.web_sockets.connect_explorer() as socket:
            try:
                socket.recv()
            finally:
                socket.close(code=1000, reason="smoke-test")

    _probe()


def _smoke_case_41() -> None:
    def _probe() -> None:
        with client.web_sockets.connect_explorer_alias() as socket:
            try:
                socket.recv()
            finally:
                socket.close(code=1000, reason="smoke-test")

    _probe()


cases: list[SmokeCase] = [
    {
        "operation": "listHealth",
        "method": "GET",
        "path": "/health",
        "run": _smoke_case_0,
    },
    {
        "operation": "createWebSocketToken",
        "method": "POST",
        "path": "/auth/ws-token",
        "run": _smoke_case_1,
    },
    {
        "operation": "list",
        "method": "GET",
        "path": "/api/{market}/price",
        "run": _smoke_case_2,
    },
    {
        "operation": "listHealth",
        "method": "GET",
        "path": "/api/{market}/health",
        "run": _smoke_case_3,
    },
    {
        "operation": "listSpotStyleStats",
        "method": "GET",
        "path": "/api/{market}/stats",
        "run": _smoke_case_4,
    },
    {
        "operation": "listPerpetualStats",
        "method": "GET",
        "path": "/api/{market}/perp_stats",
        "run": _smoke_case_5,
    },
    {
        "operation": "listTradeTapeStatus",
        "method": "GET",
        "path": "/api/{market}/trades",
        "run": _smoke_case_6,
    },
    {
        "operation": "retrieveRecentSpotTrades",
        "method": "GET",
        "path": "/api/{market}/trades/{symbol}",
        "run": _smoke_case_7,
    },
    {
        "operation": "retrieveRecentPerpetualTrades",
        "method": "GET",
        "path": "/api/{market}/perp_trades/{symbol}",
        "run": _smoke_case_8,
    },
    {
        "operation": "listDatafeedConfig",
        "method": "GET",
        "path": "/api/{market}/config",
        "run": _smoke_case_9,
    },
    {
        "operation": "listFeedTime",
        "method": "GET",
        "path": "/api/{market}/time",
        "run": _smoke_case_10,
    },
    {
        "operation": "resolveSymbol",
        "method": "GET",
        "path": "/api/{market}/symbols",
        "run": _smoke_case_11,
    },
    {
        "operation": "search",
        "method": "GET",
        "path": "/api/{market}/search",
        "run": _smoke_case_12,
    },
    {
        "operation": "listCandleHistory",
        "method": "GET",
        "path": "/api/{market}/history",
        "run": _smoke_case_13,
    },
    {
        "operation": "listQuotes",
        "method": "GET",
        "path": "/api/{market}/quotes",
        "label": "required params",
        "run": _smoke_case_14,
    },
    {
        "operation": "listQuotes",
        "method": "GET",
        "path": "/api/{market}/quotes",
        "label": "all params",
        "run": _smoke_case_15,
    },
    {
        "operation": "listHealth",
        "method": "GET",
        "path": "/api/markets/health",
        "run": _smoke_case_16,
    },
    {
        "operation": "list",
        "method": "GET",
        "path": "/api/markets/markets",
        "run": _smoke_case_17,
    },
    {
        "operation": "retrieve",
        "method": "GET",
        "path": "/api/markets/markets/{symbol}",
        "run": _smoke_case_18,
    },
    {
        "operation": "listTotals",
        "method": "GET",
        "path": "/api/markets/totals",
        "run": _smoke_case_19,
    },
    {
        "operation": "listTickers",
        "method": "GET",
        "path": "/api/markets/tickers",
        "run": _smoke_case_20,
    },
    {
        "operation": "retrieveTicker",
        "method": "GET",
        "path": "/api/markets/ticker/{symbol}",
        "run": _smoke_case_21,
    },
    {
        "operation": "listOpenInterest",
        "method": "GET",
        "path": "/api/markets/open-interest",
        "run": _smoke_case_22,
    },
    {
        "operation": "listHealth",
        "method": "GET",
        "path": "/api/explorer/health",
        "run": _smoke_case_23,
    },
    {
        "operation": "listBlocks",
        "method": "GET",
        "path": "/api/explorer/blocks",
        "run": _smoke_case_24,
    },
    {
        "operation": "listTransactions",
        "method": "GET",
        "path": "/api/explorer/transactions",
        "run": _smoke_case_25,
    },
    {
        "operation": "list",
        "method": "GET",
        "path": "/api/v3/coins/list",
        "run": _smoke_case_26,
    },
    {
        "operation": "listMarkets",
        "method": "GET",
        "path": "/api/v3/coins/markets",
        "label": "required params",
        "run": _smoke_case_27,
    },
    {
        "operation": "listMarkets",
        "method": "GET",
        "path": "/api/v3/coins/markets",
        "label": "all params",
        "run": _smoke_case_28,
    },
    {
        "operation": "retrieve",
        "method": "GET",
        "path": "/api/v3/coins/{id}",
        "run": _smoke_case_29,
    },
    {
        "operation": "listSimplePrices",
        "method": "GET",
        "path": "/api/v3/simple/price",
        "label": "required params",
        "run": _smoke_case_30,
    },
    {
        "operation": "listSimplePrices",
        "method": "GET",
        "path": "/api/v3/simple/price",
        "label": "all params",
        "run": _smoke_case_31,
    },
    {
        "operation": "listQuotes",
        "method": "GET",
        "path": "/v1/cryptocurrency/quotes/latest",
        "label": "required params",
        "run": _smoke_case_32,
    },
    {
        "operation": "listQuotes",
        "method": "GET",
        "path": "/v1/cryptocurrency/quotes/latest",
        "label": "all params",
        "run": _smoke_case_33,
    },
    {
        "operation": "listListings",
        "method": "GET",
        "path": "/v1/cryptocurrency/listings/latest",
        "run": _smoke_case_34,
    },
    {
        "operation": "listQuotesApiAlias",
        "method": "GET",
        "path": "/api/v1/cryptocurrency/quotes/latest",
        "label": "required params",
        "run": _smoke_case_35,
    },
    {
        "operation": "listQuotesApiAlias",
        "method": "GET",
        "path": "/api/v1/cryptocurrency/quotes/latest",
        "label": "all params",
        "run": _smoke_case_36,
    },
    {
        "operation": "listListingsApiAlias",
        "method": "GET",
        "path": "/api/v1/cryptocurrency/listings/latest",
        "run": _smoke_case_37,
    },
    {
        "operation": "connectPriceFeed",
        "method": "GET",
        "path": "/ws/{market}",
        "run": _smoke_case_38,
    },
    {
        "operation": "connectMarkets",
        "method": "GET",
        "path": "/ws/markets",
        "run": _smoke_case_39,
    },
    {
        "operation": "connectExplorer",
        "method": "GET",
        "path": "/ws/explorer",
        "run": _smoke_case_40,
    },
    {
        "operation": "connectExplorerAlias",
        "method": "GET",
        "path": "/explorer",
        "run": _smoke_case_41,
    },
]

DEFAULT_SMOKE_CONCURRENCY = 32


def _selected_cases() -> list[SmokeCase]:
    filter_value = os.environ.get("SCALAR_SMOKE_FILTER")
    needles = [needle.strip() for needle in filter_value.split(",") if needle.strip()] if filter_value else []
    if not needles:
        return cases
    return [case for case in cases if any(needle in case["operation"] or needle in case["path"] for needle in needles)]


def _smoke_concurrency(case_count: int) -> int:
    override = os.environ.get("SCALAR_SMOKE_CONCURRENCY")
    if override:
        try:
            parsed = int(override)
            if parsed > 0:
                return min(parsed, case_count)
        except ValueError:
            pass
    return min(DEFAULT_SMOKE_CONCURRENCY, case_count)


def _case_identity(case: SmokeCase) -> SmokeResult:
    # `label` is carried through only when the operation contributed both of its calls, so a
    # single-case operation reports exactly as it did before there were two.
    identity: SmokeResult = {
        "operation": case["operation"],
        "method": case["method"],
        "path": case["path"],
    }
    label = case.get("label")
    if label:
        identity["label"] = label
    return identity


def _run_case(case: SmokeCase) -> SmokeResult:
    started_at = time.monotonic()
    identity = _case_identity(case)
    try:
        case["run"]()
        return {
            **identity,
            "status": "passed",
            "durationMs": int((time.monotonic() - started_at) * 1000),
        }
    except Exception:
        return {
            **identity,
            "status": "failed",
            "durationMs": int((time.monotonic() - started_at) * 1000),
            "error": traceback.format_exc(),
        }


def main() -> None:
    selected = _selected_cases()
    if selected:
        # Keep enough parallelism to catch generated SDK concurrency bugs without overwhelming
        # CI runners or the in-process mock server for large SDKs.
        with ThreadPoolExecutor(max_workers=_smoke_concurrency(len(selected))) as executor:
            results = list(executor.map(_run_case, selected))
    else:
        results = []
    failed = [result for result in results if result["status"] == "failed"]

    report_path = os.environ.get("SCALAR_SMOKE_REPORT")
    if report_path:
        Path(report_path).write_text(
            json.dumps({"total": len(results), "failed": len(failed), "results": results}), encoding="utf-8"
        )
    else:
        for result in results:
            suffix = f" [{result['label']}]" if result.get("label") else ""
            if result["status"] == "passed":
                print(
                    f"PASS {result['operation']}{suffix} ({result['method']} {result['path']}) {result['durationMs']}ms"
                )
            else:
                print(
                    f"FAIL {result['operation']}{suffix} ({result['method']} {result['path']})\n{result.get('error', '')}",
                    file=sys.stderr,
                )
        if not results:
            print("No code samples ran (empty SDK or a SCALAR_SMOKE_FILTER that matched nothing).", file=sys.stderr)
        else:
            print(f"\n{len(results) - len(failed)}/{len(results)} samples passed")

    if failed or not results:
        sys.exit(1)


if __name__ == "__main__":
    main()
