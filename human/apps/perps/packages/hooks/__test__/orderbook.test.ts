import {
  getPriceKey,
  reduceOrderbook,
} from "../src/paxeer-network/useOrderbookStream";

describe("test orderbook utils", () => {
  test("test reduceOrderbook", () => {
    const result = reduceOrderbook(10, 0.001, true, {
      asks: [],
      bids: [],
    });
  });
  test("test getPriceKey", () => {
    it("rounds up (asks = true)", () => {
      expect(getPriceKey(101, 10, true)).toBe(110);
      expect(getPriceKey(100, 10, true)).toBe(100);
      expect(getPriceKey(1, 5, true)).toBe(5);
    });
    it("rounds down (asks = false)", () => {
      expect(getPriceKey(101, 10, false)).toBe(100);
      expect(getPriceKey(100, 10, false)).toBe(100);
      expect(getPriceKey(1, 5, false)).toBe(0);
    });
    it("handles depth = 1", () => {
      expect(getPriceKey(7.6, 1, true)).toBe(8);
      expect(getPriceKey(7.6, 1, false)).toBe(7);
    });
    it("handles large numbers", () => {
      expect(getPriceKey(123456789, 1000, true)).toBe(123457000);
      expect(getPriceKey(123456789, 1000, false)).toBe(123456000);
    });
  });
});
