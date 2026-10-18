const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("Multicall", function () {
  let mc;

  beforeEach(async function () {
    const Mock = await ethers.getContractFactory("MockMulticall");
    mc = await Mock.deploy();
    await mc.waitForDeployment();
  });

  it("should execute single call in batch", async function () {
    const data = [mc.interface.encodeFunctionData("setValue1", [42])];
    await mc.multicall(data);
    expect(await mc.value1()).to.equal(42);
  });

  it("should execute multiple calls in batch", async function () {
    const data = [
      mc.interface.encodeFunctionData("setValue1", [100]),
      mc.interface.encodeFunctionData("setValue2", [200]),
    ];
    await mc.multicall(data);
    expect(await mc.value1()).to.equal(100);
    expect(await mc.value2()).to.equal(200);
  });

  it("should return results from each call", async function () {
    await mc.setValue1(42);
    const data = [mc.interface.encodeFunctionData("getValue1", [])];
    // multicall is not view, so we need staticCall to get return value
    const results = await mc.multicall.staticCall(data);
    const decoded = mc.interface.decodeFunctionResult("getValue1", results[0]);
    expect(decoded[0]).to.equal(42);
  });

  it("should revert entire batch if one call fails", async function () {
    const data = [
      mc.interface.encodeFunctionData("setValue1", [100]),
      mc.interface.encodeFunctionData("revertingFunction", []),
    ];
    await expect(mc.multicall(data)).to.be.revertedWithCustomError(mc, "MulticallFailed");
  });

  it("should handle empty batch", async function () {
    await mc.multicall([]);
    // No revert = success
  });
});
