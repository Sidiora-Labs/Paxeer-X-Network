// Isolated chain; intentionally never loads repository .env or configured mainnet networks.
const toolbox=require.resolve(process.env.CONTRACT_SOURCE_ROOT+"/node_modules/@nomicfoundation/hardhat-toolbox");
require(require("node:module").createRequire(toolbox).resolve("@nomicfoundation/hardhat-ethers"));
module.exports = {solidity:"0.8.27", networks:{hardhat:{chainId:Number(process.env.FIXTURE_CHAIN_ID || 125), initialDate:"2026-10-09T00:00:00Z"}}, paths:{cache:__dirname+"/.cache", artifacts:process.env.CONTRACT_SOURCE_ROOT+"/artifacts"}};
