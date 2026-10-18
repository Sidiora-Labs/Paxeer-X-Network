require("@nomicfoundation/hardhat-toolbox");
require("dotenv/config");
if (process.env.RUN_DOCGEN === "true") require("solidity-docgen");

const optionalPlugins = [
    "hardhat-gas-reporter",
    "solidity-coverage",
    "slither",
    "hardhat-deploy",
    "hardhat-ethers",
    "hardhat-waffle",
    "hardhat-contract-sizer",
    "hardhat-abi-exporter",
];

for (const plugin of optionalPlugins) {
    try {
        require(plugin);
    } catch (error) {
        if (error.code !== "MODULE_NOT_FOUND") {
            throw error;
        }
    }
}
// Retrieve the private key and API keys from the .env file
const privateKey = process.env.PRIVATE_KEY;


// Check if the private key is set
if (!privateKey) {
    console.warn("🚨 WARNING: PRIVATE_KEY is not set in the .env file. Deployments will not be possible.");
}

const compilerSettings = {
    optimizer: {
        enabled: true,
        runs: 200,
    },
    viaIR: true,
    outputSelection: {
        "*": {
            "*": [
                "abi",
                "evm.bytecode",
                "evm.deployedBytecode",
                "evm.methodIdentifiers",
                "metadata",
                "storageLayout",
            ],
            "": ["ast"],
        },
    },
};

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
    solidity: {
        compilers: [{ version: "0.8.27", settings: compilerSettings }],
        overrides: {
            // Orders prioritizes deployment size. Settlement and EIP-170 checks remain enforced.
            "contracts/meta-ag/engine/PECOROrders.sol": {
                version: "0.8.27",
                settings: {...compilerSettings, optimizer: { enabled: true, runs: 1 } },
            },
        },
    },
    gasReporter: {
        enabled: process.env.REPORT_GAS === "true",
        currency: "USD",
    },
    networks: {
        'paxeer-x-network': {
            url: 'https://api5.mainnet-beta.paxeer.network',
            chainId: 125,
            accounts: privateKey ? [privateKey] : [],
        },
        'hardhat-network': {
            url: 'http://127.0.0.1:8545',
            // Intentionally no accounts override — falls through to the hardhat
            // node's default 20 accounts (each funded with 10,000 ETH). Account #0
            // () is the deployer recorded
            // in deployments/localhost-addresses.json, so re-deploys stay idempotent.
            chainId: 31337,
        },
    },
    etherscan: {
        apiKey: {
            'paxeer-x-network': 'empty'
        },
        customChains: [{
            network: "paxeer-x-network",
            chainId: 125,
            urls: {
                apiURL: "https://api.paxscan.io/api/",
                browserURL: "https://paxscan.io/explorer"
            }
        }]
    },
    docgen: {
        path: "docs",
        clear: true,
        runOnCompile: process.env.RUN_DOCGEN === "true",
        except: ["test/**", "mocks/**", "lib/**"],
        pages: "files",
        template: "hardhat",
        outputDir: "docs",
    },
};