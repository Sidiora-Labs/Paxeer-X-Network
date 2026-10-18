/**
 * https://docs.layerzero.network/v2/deployments/deployed-contracts
 * Chain ID to Endpoint ID mapping for LayerZero V2
 *
 * This mapping contains all supported chains and their corresponding LayerZero Endpoint IDs.
 *
 * Endpoint ID ranges:
 * - 30xxx: Mainnet chains
 * - 40xxx: Testnet chains
 */

export const CHAIN_ID_TO_ENDPOINT_ID: Record<number | string, number> = {
  // Ethereum and L2s
  1: 30101, // Ethereum Mainnet
  10: 30111, // Optimism Mainnet
  42161: 30110, // Arbitrum Mainnet
  137: 30109, // Polygon Mainnet
  8453: 30184, // Base Mainnet
  59144: 30183, // Linea Mainnet
  534352: 30214, // Scroll Mainnet
  324: 30165, // zkSync Era Mainnet
  1101: 30158, // Polygon zkEVM Mainnet
  169: 30217, // Manta Pacific Mainnet
  5000: 30181, // Mantle Mainnet
  5003: 40246, // Mantle Sepolia Testnet
  81457: 30243, // Blast Mainnet
  252: 30255, // Fraxtal Mainnet
  34443: 30260, // Mode Mainnet
  167000: 30290, // Taiko Mainnet
  7777777: 30195, // Zora Mainnet

  // Testnets
  11155420: 40232, // Optimism Sepolia Testnet
  421614: 40231, // Arbitrum Sepolia Testnet
  84532: 40245, // Base Sepolia Testnet
  300: 40305, // zkSync Sepolia Testnet
  11155111: 40161, // Ethereum Sepolia Testnet
  17000: 40217, // Ethereum Holesky Testnet

  // BNB Chain and related
  56: 30102, // BNB Smart Chain (BSC) Mainnet
  204: 30202, // opBNB Mainnet
  97: 40102, // BNB Smart Chain (BSC) Testnet

  // Avalanche
  43114: 30106, // Avalanche Mainnet
  43113: 40106, // Avalanche Fuji Testnet

  // Fantom
  250: 30112, // Fantom Mainnet

  // Gnosis
  100: 30145, // Gnosis Mainnet

  // Celo
  42220: 30125, // Celo Mainnet

  // Harmony
  1666600000: 30116, // Harmony Mainnet

  // Cronos
  25: 30359, // Cronos EVM Mainnet
  388: 30360, // Cronos zkEVM Mainnet

  // Metis
  1088: 30151, // Metis Mainnet

  // Moonbeam/Moonriver
  1284: 30126, // Moonbeam Mainnet
  1285: 30167, // Moonriver Mainnet

  // Astar
  592: 30210, // Astar Mainnet
  3776: 30257, // Astar zkEVM Mainnet

  // Canto
  7700: 30159, // Canto Mainnet

  // Fuse
  122: 30138, // Fuse Mainnet

  // Kava
  2222: 30177, // Kava Mainnet

  // Meter
  82: 30176, // Meter Mainnet

  // Near Aurora
  1313161554: 30211, // Near Aurora Mainnet

  // DFK Chain
  53935: 30115, // DFK Chain

  // DOS Chain
  7979: 30149, // DOS Chain Mainnet

  // Degen
  666666666: 30267, // Degen Mainnet

  // Dexalot Subnet
  432204: 30118, // Dexalot Subnet Mainnet

  // Homeverse
  19011: 30265, // Homeverse Mainnet

  // Horizen EON
  7332: 30215, // Horizen EON Mainnet

  // Hubble
  1992: 30182, // Hubble Mainnet

  // Tenet
  1559: 30173, // Tenet Mainnet

  // TelosEVM
  40: 30199, // TelosEVM Mainnet

  // Tiltyard
  710420: 30238, // Tiltyard Mainnet

  // Viction
  88: 30196, // Viction Mainnet

  // XPLA
  37: 30216, // XPLA Mainnet

  // Xai
  660279: 30236, // Xai Mainnet

  // re.al
  111188: 30237, // re.al Mainnet

  // inEVM
  2525: 30234, // inEVM Mainnet

  // Rari Chain
  1380012617: 30235, // Rari Chain Mainnet

  // Shimmer
  148: 30230, // Shimmer Mainnet

  // Skale
  2046399126: 30273, // Skale Mainnet

  // paxeer-network
  291: 30213, // paxeer-network Mainnet
  4460: 40200, // paxeer-network Testnet

  // CoreDAO
  1116: 30153, // CoreDAO Mainnet

  // Conflux eSpace
  1030: 30212, // Conflux eSpace Mainnet

  // Flare
  14: 30295, // Flare Mainnet

  // Gravity
  1625: 30294, // Gravity Mainnet

  // Japan Open Chain
  81: 30285, // Japan Open Chain Mainnet

  // Kaia (formerly Klaytn)
  8217: 30150, // Kaia Mainnet (formerly Klaytn)

  // OKX
  66: 30155, // OKX Mainnet

  // Beam
  4337: 30198, // Beam Mainnet

  // Loot
  5151706: 30197, // Loot Mainnet

  // Lyra
  957: 30311, // Lyra Mainnet

  // Morph
  2818: 30322, // Morph Mainnet

  // Movement
  "": 30325, // Movement Mainnet (no chain ID)

  // Bouncebit
  6001: 30293, // Bouncebit Mainnet

  // Cyber
  7560: 30283, // Cyber Mainnet

  // Iota
  8822: 30284, // Iota Mainnet

  // Sanko
  1996: 30278, // Sanko Mainnet

  // Sei
  1329: 30280, // Sei Mainnet

  // BOB
  60808: 30279, // BOB Mainnet

  // Bahamut
  5165: 30363, // Bahamut Mainnet

  // Animechain
  69000: 30372, // Animechain Mainnet

  // Ape
  33139: 30312, // Ape Mainnet

  // Berachain
  80094: 30362, // Berachain Mainnet

  // Bevm
  11501: 30317, // Bevm Mainnet

  // Bitlayer
  200901: 30314, // Bitlayer Mainnet

  // Botanix
  3637: 30376, // Botanix

  // Codex
  81224: 30323, // Codex Mainnet

  // Concrete
  12739: 30366, // Concrete

  // DM2 Verse
  68770: 30315, // DM2 Verse Mainnet

  // EDU Chain
  41923: 30328, // EDU Chain Mainnet

  // EVM on Flow
  747: 30336, // EVM on Flow Mainnet

  // Etherlink
  42793: 30292, // Etherlink Mainnet

  // Glue
  1300: 30342, // Glue Mainnet

  // Goat
  2345: 30361, // Goat Mainnet

  // Gunz
  43419: 30371, // Gunz Mainnet

  // Hedera
  295: 30316, // Hedera Mainnet

  // Hemi
  43111: 30329, // Hemi Mainnet

  // HyperEVM
  999: 30367, // HyperEVM Mainnet

  // Initia
  // "": 30326, // Initia Mainnet (no chain ID)

  // Ink
  57073: 30339, // Ink Mainnet

  // Katana
  747474: 30375, // Katana

  // Lens
  232: 30373, // Lens Mainnet

  // Lightlink
  1890: 30309, // Lightlink Mainnet

  // Lisk
  1135: 30321, // Lisk Mainnet

  // Merlin
  4200: 30266, // Merlin Mainnet

  // Nibiru
  6900: 30369, // Nibiru Mainnet

  // Otherworld Space
  8227: 30341, // Otherworld Space Mainnet

  // Peaq
  3338: 30302, // Peaq Mainnet

  // Plume
  98866: 30370, // Plume Mainnet

  // Reya
  1729: 30313, // Reya Mainnet

  // Rootstock
  30: 30333, // Rootstock Mainnet

  // Soneium
  1868: 30340, // Soneium Mainnet

  // Sonic
  146: 30332, // Sonic Mainnet

  // Sophon
  50104: 30334, // Sophon Mainnet

  // Story
  1514: 30364, // Story Mainnet

  // Subtensor EVM
  964: 30374, // Subtensor EVM Mainnet

  // Superposition
  55244: 30327, // Superposition Mainnet

  // Swell
  1923: 30335, // Swell Mainnet

  // Tac
  239: 30377, // Tac

  // TON
  // "": 30343, // TON Mainnet (no chain ID)

  // Unichain
  130: 30320, // Unichain Mainnet

  // Vana
  1480: 30330, // Vana Mainnet

  // Worldchain
  480: 30319, // Worldchain Mainnet

  // X Layer
  196: 30274, // X Layer Mainnet
  1952: 40416, // X Layer Testnet (xlayer2-testnet)

  // XChain
  94524: 30291, // XChain Mainnet

  // XDC
  50: 30365, // XDC Mainnet

  // Zircuit
  48900: 30303, // Zircuit Mainnet

  // zkLink
  810180: 30301, // zkLink Mainnet

  // Abstract
  2741: 30324, // Abstract Mainnet

  // Aptos (non-EVM)
  // 1: 30108, // Aptos Mainnet

  // Arbitrum Nova
  42170: 30175, // Arbitrum Nova Mainnet

  // Corn
  21000000: 30331, // Corn Mainnet

  // Initia Testnet
  // "": 40326, // Initia Testnet (no chain ID)

  // HyperEVM Testnet
  998: 40362, // HyperEVM Testnet

  // Berachain Bepolia Testnet
  80069: 40371, // Berachain Bepolia Testnet

  // Monad Testnet
  10143: 40204, // Monad Testnet

  // Polygon Amoy Testnet
  80002: 40267, // Polygon Amoy Testnet

  // Solana (non-EVM)
  101: 30168, // Solana Mainnet
  103: 40168, // Solana Devnet
  900900900: 30168, // Solana Mainnet
  901901901: 40168, // Solana Devnet

  // Tron
  728126428: 30420, // Tron Mainnet
  2494104990: 40420, // Tron Testnet

  // Newer mainnets (LayerZero metadata)
  2044: 30148, // Shrapnel Subnet Mainnet
  2355: 30379, // Silicon Mainnet
  5031: 30380, // Somnia Mainnet
  484: 30381, // Camp Mainnet
  6985385: 30382, // Humanity Mainnet
  9745: 30383, // Plasma Mainnet
  9069: 30384, // Apex Fusion Nexus Mainnet
  202110: 30385, // Dinari Mainnet
  1408: 30386, // zkVerify Mainnet
  16661: 30388, // 0G Mainnet
  10088: 30389, // GateLayer Mainnet
  143: 30390, // Monad Mainnet
  5064014: 30391, // Ethereal Mainnet
  1612: 30392, // Openledger Mainnet
  97477: 30393, // Doma Mainnet
  1776: 30394, // Injective EVM Mainnet
  988: 30396, // Stable Mainnet
  261131: 30397, // Zama Mainnet
  4326: 30398, // MegaETH Mainnet
  26514: 30399, // Horizen Mainnet
  4153: 30401, // Rise Mainnet
  151: 30402, // Redbelly Mainnet
  4114: 30403, // Citrea Mainnet
  2288: 30404, // Moca Mainnet
  2366: 30406, // Kite Mainnet
  1672: 30407, // Pharos Mainnet
  3282: 30408, // Irys Mainnet
  88888: 30409, // Chiliz Mainnet
  4217: 30410, // Tempo Mainnet
  685689: 30412, // Gensyn Mainnet
  904: 30413, // Ault Mainnet
  47763: 30414, // Neox Mainnet
  72957: 30415, // Rayls Mainnet
  4663: 30416, // Robinhood Chain Mainnet
  46630: 40451, // Robinhood Chain Testnet
  5042: 30417, // Arc Mainnet
  36900: 30418, // ADI Mainnet (permissionless LayerZero deployment, not in official docs; verified on-chain via RelayV2 endpoint())
  99999: 40462, // ADI Network AB Testnet
  222: 30464, // Opn Mainnet
  6714: 30465, // Anubis Mainnet
  4352: 30466, // MemeCore Mainnet
  177: 30467, // HashKey Mainnet
};

/**
 * Get Endpoint ID for a given Chain ID
 * @param chainId - The chain ID to look up
 * @returns The corresponding Endpoint ID or undefined if not found
 */
export function getEndpointId(chainId: number | string): number | undefined {
  return CHAIN_ID_TO_ENDPOINT_ID[chainId];
}
