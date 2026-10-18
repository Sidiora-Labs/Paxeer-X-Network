export interface Chain {
  id: number;
  chainNameShort: string;
  chainLogo: string;
  chainInfo: ChainInfo;
  minGasBalance: number;
  minCrossGasBalance: number;
  maxPrepayCrossGas: number;
  blockExplorerName: string;
  chainName: string;
  requestRpc: string;
}

export interface ChainInfo {
  chainId: string;
  chainName: string;
  nativeCurrency: NativeCurrency;
  rpcUrls: string[];
  blockExplorerUrls: string[];
}

export interface NativeCurrency {
  name: string;
  symbol: string;
  decimals: number;
  fix: number;
}

export const Ethereum = {
  chainNameShort: "Ethereum",
  id: 1,
  chainLogo: "",
  chainInfo: {
    chainId: `0x${(1).toString(16)}`,
    chainName: "Ethereum Mainnet",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 8,
    },
    rpcUrls: ["https://ethereum-rpc.publicnode.com"],
    blockExplorerUrls: ["https://etherscan.io/"],
  },
  minGasBalance: 0.002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.025,
  blockExplorerName: "EthScan",
  chainName: "Ethereum Mainnet",
  requestRpc: "https://rpc.ankr.com/eth",
};

export const Avalanche = {
  id: 43114,
  chainInfo: {
    chainId: `0x${(43114).toString(16)}`, // '0xa86a'
    chainName: "Avalanche",
    nativeCurrency: {
      name: "avax",
      symbol: "AVAX",
      decimals: 18,
      fix: 5,
    },
    rpcUrls: ["https://api.avax.network/ext/bc/C/rpc"],
    blockExplorerUrls: ["https://snowtrace.io/"],
  },
  minGasBalance: 0.05,
  minCrossGasBalance: 0.15,
  maxPrepayCrossGas: 2,
  blockExplorerName: "Snowtrace",
  chainName: "Avalanche",
  chainNameShort: "Avalanche",
  chainLogo: "",
  requestRpc: "https://rpc.ankr.com/avalanche",
};

export const Fuji = {
  id: 43113,
  chainInfo: {
    chainId: `0x${(43113).toString(16)}`, // '0xa86a'
    chainName: "Avalanche Fuji Testnet",
    nativeCurrency: {
      name: "avax",
      symbol: "AVAX",
      decimals: 18,
      fix: 5,
    },
    rpcUrls: ["https://api.avax.network/ext/bc/C/rpc"],
    blockExplorerUrls: ["https://snowtrace.io/"],
  },
  minGasBalance: 0.05,
  minCrossGasBalance: 0.15,
  maxPrepayCrossGas: 2,
  blockExplorerName: "Snowtrace",
  chainName: "Avalanche Fuji",
  chainNameShort: "Avalanche Fuji",
  chainLogo: "",
  requestRpc: "https://rpc.ankr.com/avalanche",
};

export const BNB = {
  id: 56,
  chainInfo: {
    chainId: `0x${(56).toString(16)}`, // 0x38
    chainName: "BNB Chain",
    nativeCurrency: {
      name: "bnb",
      symbol: "BNB",
      decimals: 18,
      fix: 6,
    },
    rpcUrls: ["https://bsc-dataseed1.binance.org/"],
    blockExplorerUrls: ["https://bscscan.com/"],
  },
  minGasBalance: 0.02,
  minCrossGasBalance: 0.02,
  maxPrepayCrossGas: 0.1,
  blockExplorerName: "BscScan",
  chainName: "Binance Smart Chain",
  chainNameShort: "BNB Chain",
  requestRpc: "https://rpc.ankr.com/bsc",
  chainLogo: "",
};

export const Fantom = {
  id: 250,
  chainInfo: {
    chainId: `0x${(250).toString(16)}`, // 0x38
    chainName: "Fantom",
    nativeCurrency: {
      name: "ftm",
      symbol: "FTM",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://rpcapi.fantom.network"],
    blockExplorerUrls: ["https://ftmscan.com/"],
  },
  minGasBalance: 1,
  minCrossGasBalance: 10,
  maxPrepayCrossGas: 60,
  blockExplorerName: "FTMScan",
  chainName: "Fantom",
  chainNameShort: "Fantom",
  chainLogo: "",
  requestRpc: "https://rpc.ankr.com/fantom",
};

export const Polygon = {
  id: 137,
  chainInfo: {
    chainId: `0x${(137).toString(16)}`,
    chainName: "Polygon",
    nativeCurrency: {
      name: "matic",
      symbol: "MATIC",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://rpc-mainnet.matic.network"],
    blockExplorerUrls: ["https://polygonscan.com/"],
  },
  minGasBalance: 0.1,
  minCrossGasBalance: 1,
  maxPrepayCrossGas: 30,
  blockExplorerName: "Polygonscan",
  chainName: "Polygon",
  chainNameShort: "Polygon",
  requestRpc: "https://rpc.ankr.com/polygon",
  chainLogo: "",
};

export const Arbitrum = {
  id: 42161,
  chainInfo: {
    chainId: `0x${(42161).toString(16)}`,
    chainName: "Arbitrum",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://arb1.arbitrum.io/rpc"],
    blockExplorerUrls: ["https://arbiscan.io/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Arbiscan",
  chainName: "Arbitrum",
  chainNameShort: "Arbitrum",
  requestRpc: "https://arb1.arbitrum.io/rpc",
  chainLogo: "",
};

export const Optimism = {
  id: 10,
  chainInfo: {
    chainId: `0x${(10).toString(16)}`,
    chainName: "Optimism",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://mainnet.optimism.io"],
    blockExplorerUrls: ["https://optimistic.etherscan.io/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Optimism",
  chainName: "Optimism",
  chainNameShort: "Optimism",
  requestRpc: "https://rpc.ankr.com/optimism",
  chainLogo: "",
};

export const zkSyncEra = {
  id: 324,
  chainInfo: {
    chainId: `0x${(324).toString(16)}`,
    chainName: "zkSync Era",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://zksync2-mainnet.zksync.io"],
    blockExplorerUrls: ["https://explorer.zksync.io/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "zkSync Era",
  chainName: "zkSync Era",
  chainNameShort: "zkSync Era",
  requestRpc: "https://zksync2-mainnet.zksync.io/",
  chainLogo: "",
  // blockExplorerName: 'zkSync Era Goerli',
  // chainName: 'zkSync Era Goerli',
  // chainNameShort: 'zkSync Era Goerli',
  // requestRpc: 'https://zksync2-testnet.zksync.dev/',
  // chainLogo:  '',
};

export const PolygonzkEVM = {
  id: 1101,
  chainInfo: {
    chainId: `0x${(1101).toString(16)}`,
    chainName: "Polygon zkEVM",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://zkevm-rpc.com"],
    blockExplorerUrls: ["https://zkevm.polygonscan.com/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Polygon zkEVM",
  chainName: "Polygon zkEVM",
  chainNameShort: "Polygon zkEVM",
  requestRpc: "https://zkevm-rpc.com",
  chainLogo: "",
};

export const Linea = {
  id: 59144,
  chainInfo: {
    chainId: `0x${(59144).toString(16)}`,
    chainName: "Linea",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://rpc.linea.build"],
    blockExplorerUrls: ["https://lineascan.build/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Linea",
  chainName: "Linea",
  chainNameShort: "Linea",
  requestRpc: "https://rpc.linea.build",
  chainLogo: "",
};

export const Base = {
  id: 8453,
  chainInfo: {
    chainId: `0x${(8453).toString(16)}`,
    chainName: "Base Network",
    nativeCurrency: {
      name: "eth",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://developer-access-mainnet.base.org/"],
    blockExplorerUrls: ["https://basescan.org"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Base",
  chainName: "Base",
  chainNameShort: "Base",
  requestRpc: "https://developer-access-mainnet.base.org/",
  chainLogo: "",
};

export const Mantle = {
  id: 5000,
  chainInfo: {
    chainId: `0x${(5000).toString(16)}`,
    chainName: "Mantle",
    nativeCurrency: {
      name: "MNT",
      symbol: "MNT",
      decimals: 6,
      fix: 4,
    },
    rpcUrls: ["https://rpc.mantle.xyz/"],
    blockExplorerUrls: ["https://mantlescan.xyz/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Mantle",
  chainName: "Mantle",
  chainNameShort: "Mantle",
  requestRpc: "https://rpc.mantle.xyz/",
  chainLogo: "",
};

export const ArbitrumGoerli = {
  id: 421613,
  chainInfo: {
    chainId: `0x${(421613).toString(16)}`,
    chainName: "Arbitrum Goerli",
    nativeCurrency: {
      name: "AGOR",
      symbol: "AGOR",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://goerli-rollup.arbitrum.io/rpc"],
    blockExplorerUrls: ["https://goerli-rollup-explorer.arbitrum.io/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Arbitrum Goerli",
  chainName: "Arbitrum Goerli",
  chainNameShort: "Arbitrum Goerli",
  requestRpc: "https://goerli-rollup.arbitrum.io/rpc",
  chainLogo: "",
};

export const ArbitrumSepolia = {
  id: 421614,
  chainInfo: {
    chainId: `0x${(421614).toString(16)}`,
    chainName: "Arbitrum Sepolia",
    nativeCurrency: {
      name: "ETH",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://arbitrum-sepolia.blockpi.network/v1/rpc/public"],
    blockExplorerUrls: ["https://sepolia-explorer.arbitrum.io"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Arbitrum Sepolia",
  chainName: "Arbitrum Sepolia",
  chainNameShort: "Arbitrum Sepolia",
  requestRpc: "https://arbitrum-sepolia.blockpi.network/v1/rpc/public",
  chainLogo: "",
};

export const OptimismGoerli = {
  id: 420,
  chainInfo: {
    chainId: `0x${(420).toString(16)}`,
    chainName: "Optimism Goerli",
    nativeCurrency: {
      name: "ETH",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://optimism-goerli.gateway.tenderly.co"],
    blockExplorerUrls: ["https://goerli-optimism.etherscan.io"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Optimism Goerli",
  chainName: "Optimism Goerli",
  chainNameShort: "Optimism Goerli",
  requestRpc: "https://optimism-goerli.gateway.tenderly.co",
  chainLogo: "",
};

export const OptimismSepolia = {
  id: 11155420,
  chainInfo: {
    chainId: `0x${(11155420).toString(16)}`,
    chainName: "Optimism Sepolia",
    nativeCurrency: {
      name: "ETH",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://sepolia.optimism.io"],
    blockExplorerUrls: ["https://sepolia-optimistic.etherscan.io"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Optimism Sepolia",
  chainName: "Optimism Sepolia",
  chainNameShort: "Optimism Sepolia",
  requestRpc: "https://sepolia.optimism.io",
  chainLogo: "",
};

export const BaseSepolia = {
  id: 84532,
  chainInfo: {
    chainId: `0x${(84532).toString(16)}`,
    chainName: "Base Sepolia",
    nativeCurrency: {
      name: "ETH",
      symbol: "ETH",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://base-sepolia-rpc.publicnode.com"],
    blockExplorerUrls: ["https://base-sepolia.blockscout.com/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Base Sepolia",
  chainName: "Base Sepolia",
  chainNameShort: "Base Sepolia",
  requestRpc: "https://base-sepolia-rpc.publicnode.com",
  chainLogo: "",
};

export const MantleSepolia = {
  id: 5003,
  chainInfo: {
    chainId: `0x${(5003).toString(16)}`,
    chainName: "Mantle Sepolia",
    nativeCurrency: {
      name: "MNT",
      symbol: "MNT",
      decimals: 6,
      fix: 4,
    },
    rpcUrls: ["https://rpc.sepolia.mantle.xyz/"],
    blockExplorerUrls: ["https://sepolia.mantlescan.xyz/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Mantle Sepolia",
  chainName: "Mantle Sepolia",
  chainNameShort: "Mantle Sepolia",
  requestRpc: "https://rpc.sepolia.mantle.xyz/",
  chainLogo: "",
};

export const PolygonAmoy = {
  id: 80002,
  chainInfo: {
    chainId: `0x${(80002).toString(16)}`,
    chainName: "Polygon Amoy",
    nativeCurrency: {
      name: "MATIC",
      symbol: "MATIC",
      decimals: 6,
      fix: 4,
    },
    rpcUrls: ["https://rpc-amoy.polygon.technology/"],
    blockExplorerUrls: ["https://www.oklink.com/amoy"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "Polygon Amoy",
  chainName: "Polygon Amoy",
  chainNameShort: "Polygon Amoy",
  requestRpc: "https://rpc-amoy.polygon.technology/",
  chainLogo: "",
};

export const Sei = {
  id: 1329,
  chainInfo: {
    chainId: `0x${(1329).toString(16)}`,
    chainName: "Sei Network",
    nativeCurrency: {
      name: "SEI",
      symbol: "SEI",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: ["https://evm-rpc.sei-apis.com"],
    blockExplorerUrls: ["https://seitrace.com/"],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "SEI",
  chainName: "Sei Network",
  chainNameShort: "SEI",
  requestRpc: "https://evm-rpc.sei-apis.com",
  chainLogo: "",
};

export const StoryTestnet = {
  name: "Story Odyssey Testnet",
  public_rpc_url: "https://rpc.odyssey.storyrpc.io/",
  chain_id: 1516,
  currency_symbol: "IP",
  explorer_base_url: "",
  vault_address: "",
};
export const StoryOdysseyTestnet = {
  id: 1329,
  chainInfo: {
    chainId: `0x${StoryTestnet.chain_id.toString(16)}`,
    chainName: StoryTestnet.name,
    nativeCurrency: {
      name: "IP",
      symbol: "IP",
      decimals: 18,
      fix: 4,
    },
    rpcUrls: [StoryTestnet.public_rpc_url],
    blockExplorerUrls: [],
  },
  minGasBalance: 0.0002,
  minCrossGasBalance: 0.002,
  maxPrepayCrossGas: 0.03,
  blockExplorerName: "SEI",
  chainName: "Sei Network",
  chainNameShort: "SEI",
  requestRpc: "https://evm-rpc.sei-apis.com",
  chainLogo: "",
};
export const chainsInfoMap: Map<number, Chain> = new Map([
  [Ethereum.id, Ethereum],
  [Avalanche.id, Avalanche],
  [Fuji.id, Fuji],
  [BNB.id, BNB],
  [Fantom.id, Fantom],
  [Polygon.id, Polygon],
  [Arbitrum.id, Arbitrum],
  [Optimism.id, Optimism],
  [zkSyncEra.id, zkSyncEra],
  [PolygonzkEVM.id, PolygonzkEVM],
  [Linea.id, Linea],
  [Base.id, Base],
  [Mantle.id, Mantle],
  [ArbitrumGoerli.id, ArbitrumGoerli],
  [ArbitrumSepolia.id, ArbitrumSepolia],
  [OptimismGoerli.id, OptimismGoerli],
  [OptimismSepolia.id, OptimismSepolia],
  [BaseSepolia.id, BaseSepolia],
  [MantleSepolia.id, MantleSepolia],
  [PolygonAmoy.id, PolygonAmoy],
  [Sei.id, Sei],
  [StoryOdysseyTestnet.id, StoryOdysseyTestnet],
]);

export const SolanaDevnet = {
  id: 901901901,
  chainInfo: {
    name: "Solana Devnet",
    public_rpc_url: "",
    chainId: 901901901,
    currency_symbol: "SOL",
    explorer_base_url: "",
    vault_address: "",
  },
} as unknown as typeof Arbitrum;

export const TestnetChains = [
  {
    network_infos: {
      name: "Arbitrum Sepolia",
      shortName: "Arbitrum Sepolia",
      public_rpc_url: "https://arbitrum-sepolia.blockpi.network/v1/rpc/public",
      chain_id: 421614,
      currency_symbol: "ETH",
      bridge_enable: true,
      mainnet: false,
      explorer_base_url: "https://sepolia.arbiscan.io",
      est_txn_mins: null,
    },
    token_infos: [
      {
        symbol: "USDC",
        address: "0x75faf114eafb1BDbe2F0316DF893fd58CE46AA4d",
        decimals: 6,
      },
    ],
  },
  {
    network_infos: {
      name: "Solana Devnet",
      public_rpc_url: "",
      chain_id: 901901901,
      currency_symbol: "SOL",
      explorer_base_url: "",
      vault_address: "",
    },
    token_infos: [
      {
        symbol: "USDC",
        address: "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
        decimals: 6,
      },
    ],
  },
  {
    network_infos: StoryTestnet,
    token_infos: [
      {
        symbol: "USDC",
        address: "0xF1815bd50389c46847f0Bda824eC8da914045D14",
        decimals: 6,
      },
    ],
  },
  // {
  //   network_infos: {
  //     name: "Mantle Sepolia",
  //     shortName: "Mantle Sepolia",
  //     public_rpc_url: "https://rpc.sepolia.mantle.xyz/",
  //     chain_id: 5003,
  //     currency_symbol: "MNT",
  //     bridge_enable: true,
  //     mainnet: false,
  //     explorer_base_url: "https://sepolia.mantlescan.xyz/",
  //     est_txn_mins: null,
  //   },
  //   token_infos: [
  //     {
  //       symbol: "USDC",
  //       address: "0xAcab8129E2cE587fD203FD770ec9ECAFA2C88080",
  //       decimals: 6,
  //       display_name: "USDC.e",
  //     },
  //   ],
  // },
  // {
  //   network_infos: {
  //     name: "Arbitrum Goerli",
  //     shortName: "Arbitrum Goerli",
  //     public_rpc_url: "https://goerli-rollup.arbitrum.io/rpc",
  //     chain_id: 421613,
  //     currency_symbol: "ETH",
  //     bridge_enable: true,
  //     mainnet: false,
  //     explorer_base_url: "https://goerli.arbiscan.io/",
  //     est_txn_mins: null,
  //   },
  //   token_infos: [
  //     {
  //       symbol: "USDC",
  //       address: "0xfd064A18f3BF249cf1f87FC203E90D8f650f2d63",
  //       decimals: 6,
  //     },
  //   ],
  // },
  // {
  //   network_infos: {
  //     name: "Optimism Goerli",
  //     shortName: "Optimism Goerli",
  //     public_rpc_url: "https://optimism-goerli.gateway.tenderly.co",
  //     chain_id: 420,
  //     currency_symbol: "ETH",
  //     bridge_enable: true,
  //     mainnet: false,
  //     explorer_base_url: "https://goerli-optimism.etherscan.io",
  //     est_txn_mins: null,
  //   },
  // },
];

export const defaultMainnetChains = [Arbitrum, Base, Optimism];
export const defaultTestnetChains = [ArbitrumSepolia];

export enum ChainNamespace {
  evm = "EVM",
  solana = "SOL",
}

export const AbstractChains = new Set([2741, 11124]);
export const SolanaChains = new Set([901901901, 900900900]);
