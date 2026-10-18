import { SwapMode } from "../types";

export function getSwapTestData(needCrossSwap: boolean) {
  return {
    mode: 0,
    src: {
      chain: 42161,
      token: "USDC.e",
      displayDecimals: 2,
      amount: "1",
      decimals: 6,
    },
    dst: {
      chain: 42161,
      token: "USDC",
      displayDecimals: 2,
      amount: "0.999959",
      decimals: 6,
    },
    chain: {
      name: "Arbitrum",
      public_rpc_url: "https://arb1.arbitrum.io/rpc",
      chain_id: 42161,
      currency_symbol: "ETH",
      bridge_enable: true,
      mainnet: true,
      explorer_base_url: "https://arbiscan.io/",
      est_txn_mins: 2,
      woofi_dex_cross_chain_router:
        "0xac8951A442fe70342f9597044B7b7657D5ad55ec",
      woofi_dex_depositor: "0x4E21a65A9F4672EF2cdfb7FA6B0f1e39d6e4E50e",
      bridgeless: true,
      shortName: "arbitrum",
    },
    nativeToken: {
      address: "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
      symbol: "ETH",
      decimals: 18,
      woofi_dex_precision: 4,
      swap_enable: true,
      precision: 4,
    },
    depositFee: 4525448866798n,
    transactionData: {
      outcomes: {
        token: "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
        symbol: "USDC",
        decimals: 6,
        amount: "999959",
      },
      fees_from: "0.000050000000000000",
      mark_prices: { from_token: 0.99980648, native_token: 2615.686055027 },
      price: 0.999959,
      route_infos: {
        tokens: [
          "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8",
          "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
        ],
        symbols: ["USDC.e", "USDC"],
        amounts: ["1000000", "999959"],
        decimals: [6, 6],
      },
      infos: {
        network: "arbitrum",
        from_token: "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8",
        from_amount: "1000000",
        to_token: "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
        min_to_amount: "989959",
      },
    },
    slippage: 1,
    brokerName: "Woofi Pro",
  };

  return {
    mode: needCrossSwap ? SwapMode.Cross : SwapMode.Single,
    src: {
      chain: 56,
      token: "BNB",
      displayDecimals: 3,
      amount: "0.224700501895331354",
      decimals: 18,
    },
    dst: {
      chain: 42161,
      token: "USDC",
      displayDecimals: 2,
      amount: "131.4744",
      decimals: 6,
    },
    chain: {
      name: "BNB Chain",
      public_rpc_url: "https://bsc-dataseed.binance.org",
      chain_id: 56,
      currency_symbol: "BNB",
      bridge_enable: true,
      mainnet: true,
      explorer_base_url: "https://bscscan.com/",
      est_txn_mins: 2,
      woofi_dex_cross_chain_router:
        "0xac8951A442fe70342f9597044B7b7657D5ad55ec",
      woofi_dex_depositor: null,
      bridgeless: false,
      shortName: "bsc",
    },
    nativeToken: {
      address: "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
      symbol: "BNB",
      decimals: 18,
      woofi_dex_precision: 3,
      swap_enable: true,
      precision: 3,
    },
    depositFee: 0n,
    transactionData: {
      dst_outcomes: {
        token: "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
        symbol: "USDC",
        decimals: 6,
        amount: "131474400",
      },
      fees_from: {
        woofi: "0.000067339180689675",
        stargate: "0.000224455929647260",
        total: "0.000291795110336935",
      },
      mark_prices: {
        from_token: 586.3354959999999,
        native_token: 586.3354959999999,
      },
      price: 585.1095075045388,
      route_infos: {
        src: {
          tokens: [
            "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
            "0x55d398326f99059fF775485246999027B3197955",
          ],
          symbols: ["BNB", "USDT"],
          amounts: ["224700501895331354", "131620160957145151463"],
          decimals: [18, 18],
        },
        dst: {
          tokens: [
            "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8",
            "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
          ],
          symbols: ["USDC.e", "USDC"],
          amounts: ["131488540", "131474400"],
          decimals: [6, 6],
        },
      },
      src_infos: {
        network: "bsc",
        from_token: "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
        from_amount: "224700501895331354",
        bridge_token: "0x55d398326f99059fF775485246999027B3197955",
        min_bridge_amount: "130962060152359419904",
      },
      dst_infos: {
        network: "arbitrum",
        chain_id: 110,
        bridged_token: "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8",
        to_token: "0xaf88d065e77c8cC2239327C5EDb3A432268e5831",
        min_to_amount: "130817028",
        gas_fee: "0.000634682448877203",
      },
    },
    slippage: 1,
    brokerName: "paxeer-network",
  };
}
