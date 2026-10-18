import { del, get, post, put } from "../src";
import { request } from "../src/fetch";

const url = "https://api.example.com/v1/test";
const originalFetch = globalThis.fetch;

const getRequestHeaders = (fetchMock: jest.Mock) => {
  const init = fetchMock.mock.calls.at(-1)?.[1] as RequestInit;
  return new Headers(init.headers);
};

describe("fetch request headers", () => {
  let fetchMock: jest.Mock;

  beforeEach(() => {
    fetchMock = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ success: true, data: { rows: [] } }),
    });
    globalThis.fetch = fetchMock as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    jest.restoreAllMocks();
  });

  it("does not add Content-Type to GET requests", async () => {
    await get(url);

    expect(getRequestHeaders(fetchMock).has("Content-Type")).toBe(false);
  });

  it.each([undefined, "get", "HEAD", "head"])(
    "does not add Content-Type when method is %s",
    async (method) => {
      await request(url, method ? { method } : {});

      expect(getRequestHeaders(fetchMock).has("Content-Type")).toBe(false);
    },
  );

  it("preserves an explicit GET Content-Type", async () => {
    await get(url, { headers: { "Content-Type": "application/custom" } });

    expect(getRequestHeaders(fetchMock).get("Content-Type")).toBe(
      "application/custom",
    );
  });

  it.each([
    ["POST", () => post(url, { value: 1 }), "application/json;charset=utf-8"],
    ["PUT", () => put(url, { value: 1 }), "application/json;charset=utf-8"],
    ["DELETE", () => del(url), "application/x-www-form-urlencoded"],
    [
      "lowercase DELETE",
      () => request(url, { method: "delete" }),
      "application/x-www-form-urlencoded",
    ],
  ])("keeps the existing %s Content-Type default", async (_name, run, type) => {
    await run();

    expect(getRequestHeaders(fetchMock).get("Content-Type")).toBe(type);
  });
});
