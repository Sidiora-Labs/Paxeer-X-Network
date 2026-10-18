const { ethers } = require("hardhat");
const {
  VIRTUAL_USDL_DEFAULT,
  VIRTUAL_TOKEN_DEFAULT,
  MIN_FEE_BPS,
  MAX_FEE_BPS,
  BASE_FEE_BPS,
  PROTOCOL_FEE_BPS,
  FEE_DECAY_RATE,
  VOLATILITY_WEIGHT,
  CONCENTRATION_WEIGHT,
  CREATION_FEE,
  TOKEN_TOTAL_SUPPLY,
} = require("./constants");

async function deployMockUSDL() {
  const MockERC20 = await ethers.getContractFactory("MockERC20");
  const usdl = await MockERC20.deploy("USD Ledger", "USDL", 6);
  await usdl.waitForDeployment();
  return usdl;
}

async function mintUSDL(usdl, to, amount) {
  await usdl.mint(to, amount);
}

async function getSigners() {
  const [deployer, alice, bob, charlie, guardian, treasury] =
    await ethers.getSigners();
  return { deployer, alice, bob, charlie, guardian, treasury };
}

module.exports = {
  deployMockUSDL,
  mintUSDL,
  getSigners,
};
