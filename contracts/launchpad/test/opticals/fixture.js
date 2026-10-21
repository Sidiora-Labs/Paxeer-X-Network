const { ethers } = require("hardhat");
async function registryFixture(admin) {
  const R = await ethers.getContractFactory("PoolRegistry");
  const implementation = await R.deploy();
  const proxy = await (await ethers.getContractFactory("UUPSProxy")).deploy(await implementation.getAddress(), R.interface.encodeFunctionData("initialize", [ethers.ZeroAddress, admin.address]));
  const registry = R.attach(await proxy.getAddress());
  await registry.grantRole(ethers.id("FACTORY_ROLE"), admin.address);
  return registry;
}
async function poolSigner(pool) {
  const address = await pool.getAddress();
  await ethers.provider.send("hardhat_setBalance", [address, "0x3635c9adc5dea00000"]);
  await ethers.provider.send("hardhat_impersonateAccount", [address]);
  return ethers.getSigner(address);
}
async function attachOptical(registry, pool, optical, creator, token) {
  await registry.register(await pool.getAddress(), token || await optical.getAddress(), creator.address, await optical.getAddress(), 1);
  return optical.connect(await poolSigner(pool));
}
module.exports = { registryFixture, poolSigner, attachOptical };
