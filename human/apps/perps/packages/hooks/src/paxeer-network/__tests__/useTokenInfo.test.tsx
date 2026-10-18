import { renderHook } from "@testing-library/react";
import { API } from "@paxeer-network/types";
import { useTokenInfo, useTokensInfo } from "../useTokensInfo/tokensInfo.store";

let mockAppTokensInfo: API.Token[];
let mockMainTokensInfo: API.Token[];
let mockTestTokensInfo: API.Token[];

jest.mock("../../provider/store/mainTokenStore", () => ({
  useMainTokenStore: (selector: (state: { data: API.Token[] }) => unknown) =>
    selector({ data: mockMainTokensInfo }),
}));

jest.mock("../../provider/store/testTokenStore", () => ({
  useTestTokenStore: (selector: (state: { data: API.Token[] }) => unknown) =>
    selector({ data: mockTestTokensInfo }),
}));

jest.mock("../appStore", () => ({
  useAppStore: (selector: (state: { tokensInfo: API.Token[] }) => unknown) =>
    selector({ tokensInfo: mockAppTokensInfo }),
}));

const createToken = (token: string, chainId: string): API.Token =>
  ({
    token,
    chain_details: [{ chain_id: chainId }],
  }) as API.Token;

const appToken = createToken("USDC", "1");
const mainnetToken = createToken("USDC", "42161");
const testnetToken = createToken("USDC", "99999");

beforeEach(() => {
  mockAppTokensInfo = [appToken];
  mockMainTokensInfo = [mainnetToken];
  mockTestTokensInfo = [testnetToken];
});

describe("useTokensInfo", () => {
  it("uses testnet token data when networkId is testnet", () => {
    const { result } = renderHook(() => useTokensInfo("testnet"));

    expect(result.current).toEqual([testnetToken]);
  });

  it("uses mainnet token data when networkId is mainnet", () => {
    const { result } = renderHook(() => useTokensInfo("mainnet"));

    expect(result.current).toEqual([mainnetToken]);
  });

  it("uses app token data when networkId is omitted", () => {
    const { result } = renderHook(() => useTokensInfo());

    expect(result.current).toEqual([appToken]);
  });
});

describe("useTokenInfo", () => {
  it("uses testnet token data when networkId is testnet", () => {
    const { result } = renderHook(() => useTokenInfo("USDC", "testnet"));

    expect(result.current).toBe(testnetToken);
  });

  it("uses mainnet token data when networkId is mainnet", () => {
    const { result } = renderHook(() => useTokenInfo("USDC", "mainnet"));

    expect(result.current).toBe(mainnetToken);
  });

  it("uses app token data when networkId is omitted", () => {
    const { result } = renderHook(() => useTokenInfo("USDC"));

    expect(result.current).toBe(appToken);
  });
});
