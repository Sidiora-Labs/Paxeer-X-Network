import { act, renderHook } from "@testing-library/react";
import useSWR from "swr";
import { ChainNamespace } from "@paxeer-network/types";
import { useDeposit } from "../useDeposit";

let mockAccount: any;
let mockChain: any;
const mockMutateAllowance = jest.fn().mockResolvedValue(undefined);

jest.mock("swr", () => ({
  __esModule: true,
  default: jest.fn(),
}));

jest.mock("../../useAccount", () => ({
  useAccount: () => ({
    account: mockAccount,
    state: {
      status: 1,
      connectWallet: { name: "Test wallet" },
    },
  }),
}));

jest.mock("../../useConfig", () => ({
  useConfig: () => "testnet",
}));

jest.mock("../../useTrack", () => ({
  useTrack: () => ({ track: jest.fn() }),
}));

jest.mock("../useChains", () => ({
  useChains: () => [
    [mockChain],
    {
      findByChainId: () => mockChain,
      isTestnetChain: () => true,
    },
  ],
}));

describe("useDeposit approve confirmation", () => {
  beforeEach(() => {
    jest.useFakeTimers();
    jest.clearAllMocks();

    mockChain = {
      network_infos: {
        chain_id: 1952,
        name: "X Layer Testnet",
        shortName: "X Layer Testnet",
        vault_address: "0xvault",
        bridgeless: true,
      },
      token_infos: [
        {
          symbol: "USDC",
          address: "0xtoken",
          decimals: 6,
        },
      ],
    };

    mockAccount = {
      walletAdapter: {
        chainNamespace: ChainNamespace.evm,
        formatUnits: jest.fn(() => "max"),
        pollTransactionReceiptWithBackoff: jest.fn(),
      },
      assetsManager: {
        approve: jest.fn(),
        deposit: jest.fn(),
        depositNativeToken: jest.fn(),
        getAllowance: jest.fn(),
        getBalance: jest.fn().mockResolvedValue("10"),
        getBalances: jest.fn().mockResolvedValue({}),
        getDepositFee: jest.fn().mockResolvedValue(0n),
        estimateDepositGasFee: jest.fn().mockResolvedValue(0n),
        estimateNativeDepositGasFee: jest.fn().mockResolvedValue(0n),
      },
    };

    (useSWR as jest.Mock).mockImplementation((key: unknown) => {
      const type = Array.isArray(key) ? key[0] : undefined;

      if (type === "allowance") {
        return {
          data: "0",
          isLoading: false,
          mutate: mockMutateAllowance,
        };
      }

      if (type === "balance") {
        return { data: "10", isLoading: false, mutate: jest.fn() };
      }

      return {
        data: 0n,
        isLoading: false,
        isValidating: false,
        mutate: jest.fn(),
      };
    });
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it("waits for a stale allowance read to reach the approved amount", async () => {
    mockAccount.assetsManager.approve.mockResolvedValue({ hash: "0xapprove" });
    mockAccount.walletAdapter.pollTransactionReceiptWithBackoff.mockResolvedValue(
      { status: 1 },
    );
    mockAccount.assetsManager.getAllowance
      .mockResolvedValueOnce("0")
      .mockResolvedValueOnce("0.65");

    const { result } = renderHook(() =>
      useDeposit({
        address: "0xtoken",
        decimals: 6,
        srcChainId: 1952,
        srcToken: "USDC",
        dstToken: "USDC",
      }),
    );

    const approval = result.current.approve("0.65");

    await act(async () => {
      await jest.advanceTimersByTimeAsync(500);
    });

    await expect(approval).resolves.toBe("0.65");
    expect(mockAccount.assetsManager.getAllowance).toHaveBeenCalledTimes(2);
    expect(mockMutateAllowance).toHaveBeenCalledWith("0.65", {
      revalidate: false,
    });
  });

  it("rejects a failed approve receipt before checking allowance", async () => {
    mockAccount.assetsManager.approve.mockResolvedValue({ hash: "0xapprove" });
    mockAccount.walletAdapter.pollTransactionReceiptWithBackoff.mockResolvedValue(
      { status: 0 },
    );

    const { result } = renderHook(() =>
      useDeposit({
        address: "0xtoken",
        decimals: 6,
        srcChainId: 1952,
        srcToken: "USDC",
        dstToken: "USDC",
      }),
    );

    await expect(result.current.approve("0.65")).rejects.toThrow(
      "Transaction failed",
    );
    expect(mockAccount.assetsManager.getAllowance).not.toHaveBeenCalled();
  });

  it("retries a stale allowance read before sending deposit", async () => {
    mockAccount.assetsManager.approve.mockResolvedValue({ hash: "0xapprove" });
    mockAccount.assetsManager.getAllowance
      .mockResolvedValueOnce("0.65")
      .mockResolvedValueOnce("0")
      .mockResolvedValueOnce("0.65")
      .mockResolvedValue("0.65");
    mockAccount.assetsManager.deposit.mockResolvedValue({ hash: "0xdeposit" });
    mockAccount.walletAdapter.pollTransactionReceiptWithBackoff.mockResolvedValue(
      { status: 1 },
    );

    const { result } = renderHook(() =>
      useDeposit({
        address: "0xtoken",
        decimals: 6,
        srcChainId: 1952,
        srcToken: "USDC",
        dstToken: "USDC",
      }),
    );

    act(() => {
      result.current.setQuantity("0.65");
    });

    await act(async () => {
      await result.current.approve("0.65");
    });

    const deposit = result.current.deposit();
    await act(async () => {
      await jest.advanceTimersByTimeAsync(500);
    });

    await expect(deposit).resolves.toEqual({ hash: "0xdeposit" });
    expect(mockAccount.assetsManager.deposit).toHaveBeenCalledTimes(1);
  });

  it("does not send deposit when allowance never becomes sufficient", async () => {
    mockAccount.assetsManager.getAllowance.mockResolvedValue("0");

    const { result } = renderHook(() =>
      useDeposit({
        address: "0xtoken",
        decimals: 6,
        srcChainId: 1952,
        srcToken: "USDC",
        dstToken: "USDC",
      }),
    );

    act(() => {
      result.current.setQuantity("0.65");
    });

    const deposit = result.current.deposit();
    const rejection = expect(deposit).rejects.toThrow("Insufficient allowance");

    await act(async () => {
      await jest.advanceTimersByTimeAsync(7500);
    });

    await rejection;
    expect(mockAccount.assetsManager.deposit).not.toHaveBeenCalled();
  });
});
