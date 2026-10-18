import { act, renderHook } from "@testing-library/react";
import type { API } from "@paxeer-network/types";
import { useChains } from "../useChains";

let mockMainnetChainInfos: API.NetworkInfos[];
let mockTestnetChainInfos: API.NetworkInfos[];
let mockMainnetChainInfoOrigin: "broker" | "generic";
let mockTestnetChainInfoOrigin: "broker" | "generic";
let mockMainnetTokens: API.Token[];
let mockTestnetTokens: API.Token[] | null;

jest.mock("../../provider/store/chainInfoMainStore", () => ({
  useMainnetChainsStore: (
    selector: (state: {
      data: API.NetworkInfos[];
      dataOrigin: "broker" | "generic";
    }) => unknown,
  ) =>
    selector({
      data: mockMainnetChainInfos,
      dataOrigin: mockMainnetChainInfoOrigin,
    }),
}));

jest.mock("../../provider/store/chainInfoTestStore", () => ({
  useTestnetChainsStore: (
    selector: (state: {
      data: API.NetworkInfos[];
      dataOrigin: "broker" | "generic";
    }) => unknown,
  ) =>
    selector({
      data: mockTestnetChainInfos,
      dataOrigin: mockTestnetChainInfoOrigin,
    }),
}));

jest.mock("../../provider/store/mainTokenStore", () => ({
  useMainTokenStore: () => ({ data: mockMainnetTokens, error: null }),
}));

jest.mock("../../provider/store/testTokenStore", () => ({
  useTestTokenStore: (
    selector: (state: { data: API.Token[] | null }) => unknown,
  ) => selector({ data: mockTestnetTokens }),
}));

const TESTNET_CHAIN_ID = 123456;
const UPDATED_TESTNET_CHAIN_ID = 123457;
const MAINNET_CHAIN_ID = 42161;

const createChainInfo = (chainId: number, name: string): API.NetworkInfos =>
  ({
    chain_id: chainId,
    name,
    public_rpc_url: "",
    currency_symbol: "ETH",
    currency_decimal: 18,
    explorer_base_url: "",
    vault_address: "",
  }) as API.NetworkInfos;

beforeEach(() => {
  mockMainnetChainInfos = [createChainInfo(MAINNET_CHAIN_ID, "Mainnet")];
  mockTestnetChainInfos = [createChainInfo(TESTNET_CHAIN_ID, "Testnet")];
  mockMainnetChainInfoOrigin = "broker";
  mockTestnetChainInfoOrigin = "broker";
  mockMainnetTokens = [];
  mockTestnetTokens = [];
});

describe("useChains empty data", () => {
  test("returns a stable grouped shape while data is unavailable", () => {
    mockTestnetTokens = null;

    const { result } = renderHook(() => useChains());

    expect(result.current[0]).toEqual({ mainnet: [], testnet: [] });
  });
});

describe("useChains chain-info authority", () => {
  test("does not expose generic wallet-init data for chain selection", () => {
    const genericMainnetChainId = 777777;
    mockMainnetChainInfos = [
      createChainInfo(genericMainnetChainId, "Generic Mainnet"),
    ];
    mockMainnetChainInfoOrigin = "generic";
    mockTestnetChainInfoOrigin = "generic";

    const { result } = renderHook(() => useChains());

    expect(result.current[1].findByChainId(TESTNET_CHAIN_ID)).toBeUndefined();
    expect(
      result.current[1].findByChainId(genericMainnetChainId),
    ).toBeUndefined();
    expect(result.current[1].findByChainId(MAINNET_CHAIN_ID)).toBeDefined();
    expect(result.current[1].findByChainId(421614)).toBeDefined();
  });
});

describe("useChains isTestnetChain", () => {
  test.each([
    TESTNET_CHAIN_ID,
    TESTNET_CHAIN_ID.toString(),
    `0x${TESTNET_CHAIN_ID.toString(16)}`,
  ])("identifies a testnet API chain from chain id %s", (chainId) => {
    const { result } = renderHook(() => useChains());

    expect(result.current[1].isTestnetChain(chainId)).toBe(true);
  });

  test("does not identify a mainnet API chain as testnet", () => {
    const { result } = renderHook(() => useChains());

    expect(result.current[1].isTestnetChain(MAINNET_CHAIN_ID)).toBe(false);
  });

  test("replaces stale chain data when the API list changes", () => {
    const { result, rerender } = renderHook(() => useChains());

    expect(result.current[1].isTestnetChain(TESTNET_CHAIN_ID)).toBe(true);
    expect(result.current[1].findByChainId(TESTNET_CHAIN_ID)).toBeDefined();

    act(() => {
      mockTestnetChainInfos = [
        createChainInfo(UPDATED_TESTNET_CHAIN_ID, "Updated Testnet"),
      ];
      rerender();
    });

    expect(result.current[1].isTestnetChain(TESTNET_CHAIN_ID)).toBe(false);
    expect(result.current[1].findByChainId(TESTNET_CHAIN_ID)).toBeUndefined();
    expect(result.current[1].isTestnetChain(UPDATED_TESTNET_CHAIN_ID)).toBe(
      true,
    );
    expect(
      result.current[1].findByChainId(UPDATED_TESTNET_CHAIN_ID),
    ).toBeDefined();
  });

  test("clears chain data when the API list becomes empty", () => {
    const { result, rerender } = renderHook(() => useChains());

    expect(result.current[1].findByChainId(TESTNET_CHAIN_ID)).toBeDefined();

    act(() => {
      mockTestnetChainInfos = [];
      rerender();
    });

    expect(result.current[1].isTestnetChain(TESTNET_CHAIN_ID)).toBe(false);
    expect(result.current[1].findByChainId(TESTNET_CHAIN_ID)).toBeUndefined();
  });

  test.each([999999, "", "invalid", undefined])(
    "returns false for an unknown or invalid chain id %s",
    (chainId) => {
      const { result } = renderHook(() => useChains());

      expect(result.current[1].isTestnetChain(chainId)).toBe(false);
    },
  );
});
