import { waitForAllowance } from "../allowance";

describe("waitForAllowance", () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it("resolves immediately when the allowance is sufficient", async () => {
    const readAllowance = jest.fn().mockResolvedValue("1");
    const onSatisfied = jest.fn().mockResolvedValue(undefined);

    await expect(
      waitForAllowance({
        readAllowance,
        expectedAmount: "0.65",
        onSatisfied,
      }),
    ).resolves.toBe("1");

    expect(readAllowance).toHaveBeenCalledTimes(1);
    expect(onSatisfied).toHaveBeenCalledWith("1");
  });

  it("retries stale allowance reads before resolving", async () => {
    const readAllowance = jest
      .fn()
      .mockResolvedValueOnce("0")
      .mockResolvedValueOnce("0.65");

    const promise = waitForAllowance({
      readAllowance,
      expectedAmount: "0.65",
    });

    await jest.advanceTimersByTimeAsync(500);

    await expect(promise).resolves.toBe("0.65");
    expect(readAllowance).toHaveBeenCalledTimes(2);
  });

  it("retries temporary RPC errors before resolving", async () => {
    const readAllowance = jest
      .fn()
      .mockRejectedValueOnce(new Error("RPC unavailable"))
      .mockResolvedValueOnce("1");

    const promise = waitForAllowance({
      readAllowance,
      expectedAmount: "0.65",
    });

    await jest.advanceTimersByTimeAsync(500);

    await expect(promise).resolves.toBe("1");
    expect(readAllowance).toHaveBeenCalledTimes(2);
  });

  it("rejects after all allowance reads remain insufficient", async () => {
    const readAllowance = jest.fn().mockResolvedValue("0");

    const promise = waitForAllowance({
      readAllowance,
      expectedAmount: "0.65",
    });
    const rejection = expect(promise).rejects.toThrow("Insufficient allowance");

    await jest.advanceTimersByTimeAsync(7500);

    await rejection;
    expect(readAllowance).toHaveBeenCalledTimes(6);
  });

  it("supports exact-zero confirmation for reset approve", async () => {
    const readAllowance = jest.fn().mockResolvedValue("0");

    await expect(
      waitForAllowance({
        readAllowance,
        expectedAmount: "0",
        comparison: "eq",
      }),
    ).resolves.toBe("0");
  });
});
